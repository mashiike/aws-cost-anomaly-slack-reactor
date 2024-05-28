package reactor

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/aws/aws-lambda-go/lambdacontext"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/gorilla/mux"
	"github.com/mashiike/canyon"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

type Handler struct {
	ce                *costexplorer.Client
	org               *organizations.Client
	ddb               *dynamodb.Client
	client            *slack.Client
	logger            *slog.Logger
	router            *mux.Router
	channel           string
	botUserID         string
	botID             string
	slackTeamID       string
	signalSecret      string
	awsAccountID      string
	noErrorReport     bool
	tpl               *template.Template
	dynamodbTableName string
}

var _ http.Handler = (*Handler)(nil)

//go:embed default_message.json.tpl
var defaultTemplate string

func New(ctx context.Context, opts ...Option) (*Handler, error) {
	token, ok := os.LookupEnv("SLACK_TOKEN")
	if !ok {
		token = os.Getenv("SLACK_BOT_TOKEN")
	}
	params := &optionParams{
		slackBotToken:     token,
		slackChannel:      os.Getenv("SLACK_CHANNEL"),
		logger:            slog.Default(),
		slackSignalSecret: os.Getenv("SLACK_SIGNING_SECRET"),
		templateStr:       defaultTemplate,
	}
	for _, opt := range opts {
		opt(params)
	}
	if params.slackBotToken == "" {
		return nil, errors.New("slack bot token is required")
	}
	if params.slackChannel == "" {
		return nil, errors.New("slack channel is required")
	}
	if params.slackSignalSecret == "" {
		return nil, errors.New("slack signing secret is required")
	}
	if params.templateStr == "" {
		return nil, errors.New("template string is required")
	}
	tpl, err := template.New("default").Funcs(template.FuncMap{
		"env": func(key string, args ...string) string {
			keys := []string{key}
			defaultValue := ""
			if len(args) > 1 {
				defaultValue = args[len(args)-1]
				keys = append(keys, args[:len(args)-1]...)
			}
			for _, k := range keys {
				if v := os.Getenv(k); v != "" {
					return v
				}
			}
			return defaultValue
		},
		"must_env": func(key string) (string, error) {
			if v, ok := os.LookupEnv(key); ok {
				return v, nil
			}
			return "", fmt.Errorf("environment variable %s is not set", key)
		},
		"json_escape": func(str string) (string, error) {
			bs, err := json.Marshal(str)
			if err != nil {
				return "", err
			}
			return string([]byte(bs)[1 : len(bs)-1]), nil
		},
		"to_date_str": func(t time.Time) string {
			return t.Format("2006-01-02")
		},
	}).Parse(params.templateStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}
	if params.awsCfg == nil {
		awsCfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			return nil, err
		}
		params.awsCfg = &awsCfg
	}
	stsClient := sts.NewFromConfig(*params.awsCfg)
	var awsAccountID string
	if identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{}); err == nil {
		awsAccountID = *identity.Account
	} else {
		params.logger.Debug("failed to get aws account id", "error", err)
	}

	var botID, botUserID, teamID string
	client := slack.New(params.slackBotToken)
	if params.slackBotToken != "" {
		me, err := client.AuthTest()
		if err != nil {
			return nil, err
		}
		params.logger.Info("running sloack bot",
			"bot_id", me.BotID,
			"bot_user_id", me.UserID,
			"team_id", me.TeamID,
			"user", me.User,
			"team", me.Team,
			"enterprise_id", me.EnterpriseID,
		)
		botID = me.BotID
		botUserID = me.UserID
		teamID = me.TeamID
	} else {
		params.logger.Warn("slack bot token is not set, running anonymous mode")
	}
	router := mux.NewRouter()
	h := &Handler{
		ce:                costexplorer.NewFromConfig(*params.awsCfg),
		org:               organizations.NewFromConfig(*params.awsCfg),
		ddb:               dynamodb.NewFromConfig(*params.awsCfg),
		logger:            params.logger.With("component", "handler"),
		router:            router,
		client:            client,
		botID:             botID,
		channel:           params.slackChannel,
		botUserID:         botUserID,
		slackTeamID:       teamID,
		signalSecret:      params.slackSignalSecret,
		awsAccountID:      awsAccountID,
		noErrorReport:     params.noErrorReport,
		dynamodbTableName: params.dynamodbTableName,
		tpl:               tpl,
	}
	if h.EnableDynamoDB() {
		params.logger.Info("dynamodb enabled", "table_name", h.dynamodbTableName)
		if err := h.PrepareDynamoDBTable(ctx); err != nil {
			return nil, fmt.Errorf("failed to prepare dynamodb table: %w", err)
		}
	}
	var dummy templateData
	if _, err := h.newDetectAnomalyMessageOptions(dummy); err != nil {
		return nil, fmt.Errorf("failed to create default message: %w", err)
	}
	router.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	router.HandleFunc("/amazon-sns", h.handleAmazonSNS).Methods(http.MethodPost)
	router.HandleFunc("/slack/events", h.handleSlackEvents).Methods(http.MethodPost)
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.UserAgent(), "Slackbot") {
			h.handleSlackEvents(w, r)
			return
		}
		if strings.Contains(r.UserAgent(), "Amazon Simple Notification Service") {
			h.handleAmazonSNS(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}).Methods(http.MethodPost)
	return h, nil
}

func (h *Handler) EnableDynamoDB() bool {
	return h.dynamodbTableName != ""
}

func (h *Handler) PrepareDynamoDBTable(ctx context.Context) error {
	h.logger.DebugContext(ctx, "prepare dynamodb table", "table_name", h.dynamodbTableName)
	// check table exists
	describeOutput, err := h.ddb.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(h.dynamodbTableName),
	})
	if err != nil {
		var notFound *ddbtypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			h.logger.InfoContext(ctx, "table not found, create table", "table_name", h.dynamodbTableName)
			createOutput, err := h.ddb.CreateTable(ctx, &dynamodb.CreateTableInput{
				TableName: aws.String(h.dynamodbTableName),
				KeySchema: []ddbtypes.KeySchemaElement{
					{
						AttributeName: aws.String("AnomalyID"),
						KeyType:       ddbtypes.KeyTypeHash,
					},
					{
						AttributeName: aws.String("SlackTeamID"),
						KeyType:       ddbtypes.KeyTypeRange,
					},
				},
				AttributeDefinitions: []ddbtypes.AttributeDefinition{
					{
						AttributeName: aws.String("AnomalyID"),
						AttributeType: ddbtypes.ScalarAttributeTypeS,
					},
					{
						AttributeName: aws.String("SlackTeamID"),
						AttributeType: ddbtypes.ScalarAttributeTypeS,
					},
				},
				BillingMode: ddbtypes.BillingModePayPerRequest,
			})
			if err != nil {
				return fmt.Errorf("failed to create table: %w", err)
			}
			describeOutput = &dynamodb.DescribeTableOutput{
				Table: createOutput.TableDescription,
			}
		} else {
			return fmt.Errorf("failed to describe table: %w", err)
		}
	}
	waiter := func() (bool, error) {
		timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		// wait table ready
		for describeOutput.Table.TableStatus != ddbtypes.TableStatusActive {
			select {
			case <-timeoutCtx.Done():
				return false, fmt.Errorf("timeout")
			default:
			}
			h.logger.DebugContext(timeoutCtx, "wait table ready", "table_status", describeOutput.Table.TableStatus)
			time.Sleep(100 * time.Millisecond)
			describeOutput, err = h.ddb.DescribeTable(timeoutCtx, &dynamodb.DescribeTableInput{
				TableName: aws.String(h.dynamodbTableName),
			})
			if err != nil {
				return false, fmt.Errorf("failed to describe table: %w", err)
			}
		}
		return true, nil
	}
	if ok, err := waiter(); err != nil {
		return fmt.Errorf("failed to wait table ready: %w", err)
	} else if !ok {
		return fmt.Errorf("table not ready")
	}
	h.logger.InfoContext(ctx, "table ready", "table_name", h.dynamodbTableName)
	// check ttl enabled
	desc, err := h.ddb.DescribeTimeToLive(ctx, &dynamodb.DescribeTimeToLiveInput{
		TableName: aws.String(h.dynamodbTableName),
	})
	if err != nil {
		return fmt.Errorf("failed to describe ttl: %w", err)
	}
	if desc.TimeToLiveDescription.TimeToLiveStatus != ddbtypes.TimeToLiveStatusEnabled {
		h.logger.InfoContext(ctx, "enable ttl", "table_name", h.dynamodbTableName)
		_, err := h.ddb.UpdateTimeToLive(ctx, &dynamodb.UpdateTimeToLiveInput{
			TableName: aws.String(h.dynamodbTableName),
			TimeToLiveSpecification: &ddbtypes.TimeToLiveSpecification{
				AttributeName: aws.String("TTL"),
				Enabled:       aws.Bool(true),
			},
		})
		if err != nil {
			return fmt.Errorf("failed to enable ttl: %w", err)
		}
	}
	return nil
}

type AnomalySlackMessage struct {
	AnomalyID             string
	SlackTeamID           string
	SlackMessageTimestamp string
	TotalImpact           float64
	TTL                   time.Time
}

func (h *Handler) SaveAnomalySlackMessage(ctx context.Context, m *AnomalySlackMessage) error {
	m.SlackTeamID = h.slackTeamID
	m.TTL = time.Now().AddDate(0, 1, 0)
	h.logger.DebugContext(ctx, "save anomaly slack message", "anomaly_id", m.AnomalyID, "slack_team_id", m.SlackTeamID)
	item, err := attributevalue.MarshalMap(m)
	if err != nil {
		return fmt.Errorf("failed to marshal item: %w", err)
	}
	expr, err := expression.NewBuilder().
		WithCondition(
			expression.AttributeNotExists(expression.Name("AnomalyID")).And(expression.AttributeNotExists(expression.Name("SlackTeamID"))),
		).
		Build()
	if err != nil {
		return fmt.Errorf("failed to build expression: %w", err)
	}
	_, err = h.ddb.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:                aws.String(h.dynamodbTableName),
		Item:                     item,
		ConditionExpression:      expr.Condition(),
		ExpressionAttributeNames: expr.Names(),
	})
	if err != nil {
		return fmt.Errorf("failed to put item: %w", err)
	}
	return nil
}

func (h *Handler) GetAnomalySlackMessage(ctx context.Context, anomalyID string) (*AnomalySlackMessage, bool, error) {
	h.logger.DebugContext(ctx, "get anomaly slack message", "anomaly_id", anomalyID, "slack_team_id", h.slackTeamID)
	output, err := h.ddb.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(h.dynamodbTableName),
		Key: map[string]ddbtypes.AttributeValue{
			"AnomalyID":   &ddbtypes.AttributeValueMemberS{Value: anomalyID},
			"SlackTeamID": &ddbtypes.AttributeValueMemberS{Value: h.slackTeamID},
		},
	})
	if err != nil {
		var notFound *ddbtypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to get item: %w", err)
	}
	var m AnomalySlackMessage
	if err := attributevalue.UnmarshalMap(output.Item, &m); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal item: %w", err)
	}
	if m.AnomalyID == "" || m.SlackTeamID == "" {
		return nil, false, nil
	}
	return &m, true, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("serve http", "method", r.Method, "path", r.URL.Path, "remote_addr", r.RemoteAddr, "user_agent", r.UserAgent(), "referer", r.Referer())
	h.router.ServeHTTP(w, r)
}

// https://docs.aws.amazon.com/sns/latest/dg/json-formats.html
type httpNotification struct {
	Type             string    `json:"Type"`
	MessageId        string    `json:"MessageId"`
	Token            string    `json:"Token,omitempty"`
	TopicArn         string    `json:"TopicArn"`
	Subject          string    `json:"Subject,omitempty"`
	Message          string    `json:"Message"`
	SubscribeURL     string    `json:"SubscribeURL,omitempty"`
	Timestamp        time.Time `json:"Timestamp"`
	SignatureVersion string    `json:"SignatureVersion"`
	Signature        string    `json:"Signature"`
	SigningCertURL   string    `json:"SigningCertURL"`
	UnsubscribeURL   string    `json:"UnsubscribeURL,omitempty"`
}

const (
	actionsBlockID          = "aws-cost-anomaly-detection-reactor"
	actionsYesID            = "yes"
	actionsNoID             = "no"
	actionsPlanedActivityID = "planed_activity"
)

type templateData struct {
	Anomaly                    Anomaly
	MonitorID                  string
	ActionsBlockID             string
	ActionsYesValue            string
	ActionsYesID               string
	ActionsNoValue             string
	ActionsNoID                string
	ActionsPlanedActivityValue string
	ActionsPlanedActivityID    string
}

func (h *Handler) newTemplateData(_ context.Context, anomaly Anomaly) (templateData, error) {
	var monitorID string
	arnObj, err := arn.Parse(anomaly.MonitorArn)
	if err != nil {
		return templateData{}, fmt.Errorf("failed to parse monitor arn: %w", err)
	}
	monitorID = strings.TrimPrefix(arnObj.Resource, "anomalymonitor/")
	data := templateData{
		Anomaly:        anomaly,
		MonitorID:      monitorID,
		ActionsBlockID: actionsBlockID,
		ActionsYesValue: url.Values{
			"anomaly_id": []string{anomaly.AnomalyID},
			"action":     []string{string(types.AnomalyFeedbackTypeYes)},
		}.Encode(),
		ActionsYesID: actionsYesID,
		ActionsNoValue: url.Values{
			"anomaly_id": []string{anomaly.AnomalyID},
			"action":     []string{string(types.AnomalyFeedbackTypeNo)},
		}.Encode(),
		ActionsNoID: actionsNoID,
		ActionsPlanedActivityValue: url.Values{
			"anomaly_id": []string{anomaly.AnomalyID},
			"action":     []string{string(types.AnomalyFeedbackTypePlannedActivity)},
		}.Encode(),
		ActionsPlanedActivityID: actionsPlanedActivityID,
	}
	return data, nil
}

func (h *Handler) newDetectAnomalyMessageOptions(data templateData) ([]slack.MsgOption, error) {
	var buf bytes.Buffer
	if err := h.tpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}
	var msg slack.Msg
	dec := json.NewDecoder(&buf)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&msg); err != nil {
		return nil, fmt.Errorf("failed to decode template: %w", err)
	}
	opts := make([]slack.MsgOption, 0, 3)
	if msg.Text != "" {
		opts = append(opts, slack.MsgOptionText(msg.Text, false))
	}
	if len(msg.Attachments) > 0 {
		opts = append(opts, slack.MsgOptionAttachments(msg.Attachments...))
	}
	if len(msg.Blocks.BlockSet) > 0 {
		opts = append(opts, slack.MsgOptionBlocks(msg.Blocks.BlockSet...))
	}
	return opts, nil
}

func (h *Handler) handleAmazonSNS(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug("start handle amazon sns")
	bs, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("failed to read body", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	slog.Debug("amazon sns notification", "body", string(bs))
	var n httpNotification
	dec := json.NewDecoder(bytes.NewBuffer(bs))
	if err := dec.Decode(&n); err != nil {
		h.logger.Error("failed to decode body", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	h.logger.Info("handle amazon sns http notification", "type", n.Type, "topic_arn", n.TopicArn, "subject", n.Subject)
	if n.Type == "" && n.MessageId == "" && n.TopicArn == "" {
		h.logger.Warn("maybe this is raw notification, fallbac as notification type")
		n.Message = string(bs)
		n.Type = "Notification"
	}
	switch n.Type {
	case "SubscriptionConfirmation":
		h.logger.Info("subscription confirmation", "subscribe_url", n.SubscribeURL)
		arnObj, err := arn.Parse(n.TopicArn)
		if err != nil {
			h.logger.Error("failed to parse arn", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		client := sns.New(sns.Options{Region: arnObj.Region})
		_, err = client.ConfirmSubscription(ctx, &sns.ConfirmSubscriptionInput{
			Token:                     aws.String(n.Token),
			TopicArn:                  aws.String(n.TopicArn),
			AuthenticateOnUnsubscribe: aws.String("no"),
		})
		if err != nil {
			h.logger.Error("failed to confirm subscription", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		h.logger.Info("confirmed subscription", "topic_arn", n.TopicArn)
		_, _, err = h.client.PostMessage(h.channel, slack.MsgOptionText(fmt.Sprintf("confirmed sns subscription for %s", n.TopicArn), false))
		if err != nil {
			h.logger.Error("failed to post message", "error", err)
		}
		w.WriteHeader(http.StatusOK)
		return
	case "Notification":
		h.logger.Info("handle amazon sns notification", "subject", n.Subject, "message_id", n.MessageId)
		var a Anomaly
		if err := json.Unmarshal([]byte(n.Message), &a); err != nil {
			h.logger.Error("failed to unmarshal message", "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := h.postAnomalyDetectedMessage(ctx, a); err != nil {
			h.logger.Error("failed to post anomaly detected message", "error", err)
			if !h.noErrorReport {
				_, _, err := h.client.PostMessage(h.channel, slack.MsgOptionText(fmt.Sprintf("[error] failed to post anomaly detected message: %s", err), false))
				if err != nil {
					h.logger.Error("failed to post message", "error", err)
				}
			}
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func (h *Handler) handleSlackEvents(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug("start handle slack events")
	verifier, err := slack.NewSecretsVerifier(r.Header, h.signalSecret)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	bs, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("failed to read body", "error", err)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	r.Body.Close()
	r.Body = io.NopCloser(strings.NewReader(string(bs)))

	if _, err := verifier.Write(bs); err != nil {
		h.logger.Error("failed to write for verify", "error", err)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if err := verifier.Ensure(); err != nil {
		h.logger.Error("failed to verify", "error", err)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
		h.processInteractiveMessage(w, r)
	} else {
		h.processEventsAPIEvent(w, r)
	}
}

func (h *Handler) processInteractiveMessage(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug("process interactive message")
	var payload slack.InteractionCallback
	err := json.Unmarshal([]byte(r.FormValue("payload")), &payload)
	if err != nil {
		fmt.Printf("Could not parse action response JSON: %v", err)
	}

	if canyon.Used(r) && !canyon.IsWorker(r) {
		msgID, err := canyon.SendToWorker(r, nil)
		if err != nil {
			h.logger.Error("failed to send to worker", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		h.logger.Info("send process intaracitve message request to worker", "msg_id", msgID)
		w.WriteHeader(http.StatusOK)
		return
	}
	isWorker := canyon.Used(r) && canyon.IsWorker(r)
	postToThread := func(ctx context.Context, options ...slack.MsgOption) error {
		msgTs := payload.Message.Timestamp
		msgChannel := payload.Channel.ID
		options = append(options, slack.MsgOptionTS(msgTs))
		_, _, err := h.client.PostMessageContext(ctx, msgChannel, options...)
		if err != nil {
			return fmt.Errorf("failed to post message: %w", err)
		}
		return nil
	}
	ctx := r.Context()
	var action *slack.BlockAction
	if len(payload.ActionCallback.BlockActions) == 0 {
		h.logger.Warn("no action found")
		w.WriteHeader(http.StatusOK)
		return
	}
	actionUser := payload.User
	for _, a := range payload.ActionCallback.BlockActions {
		if a.BlockID == actionsBlockID {
			action = a
			break
		}
	}
	if action == nil {
		h.logger.Warn("no action found")
		w.WriteHeader(http.StatusOK)
		return
	}

	v, err := url.ParseQuery(action.Value)
	if err != nil {
		h.logger.Warn("failed to parse action value", "error", err)
		if isWorker {
			postToThread(ctx, slack.MsgOptionText(fmt.Sprintf("[error] failed to parse action value: %s", err), false))
		}
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	anomalyID := v.Get("anomaly_id")
	h.logger.Info("provide feedback action", "anomaly_id", anomalyID, "action_id", action.ActionID, "user_id", actionUser.ID)
	if err := h.ProvideFeedback(ctx, anomalyID, action.ActionID); err != nil {
		h.logger.Error("failed to provide feedback", "error", err)
		if isWorker {
			postToThread(ctx,
				slack.MsgOptionText(fmt.Sprintf("[error] failed to provide feedback: %s", err), false),
			)
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	postToThread(ctx,
		slack.MsgOptionText(fmt.Sprintf("Feedback of `%s` was provided for AnomalyID `%s` by user `%s` .", action.Text.Text, anomalyID, actionUser.Name), false),
		slack.MsgOptionBroadcast(),
	)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) processEventsAPIEvent(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug("process events api event")
	bs, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("failed to read body", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	eventsAPIEvent, err := slackevents.ParseEvent(json.RawMessage(bs), slackevents.OptionNoVerifyToken())
	if err != nil {
		h.logger.Error("failed to parse event", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	h.logger.Debug("events api event", "type", eventsAPIEvent.Type)
	switch eventsAPIEvent.Type {
	case slackevents.URLVerification:
		h.logger.Debug("url verification")
		var r *slackevents.ChallengeResponse
		err := json.Unmarshal(bs, &r)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(r.Challenge))
		h.logger.Info("url verification success")
		return
	case slackevents.CallbackEvent:
		innerEvent := eventsAPIEvent.InnerEvent
		switch ev := innerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			h.logger.Info("app mention event", "text", ev.Text)
			var builder strings.Builder
			if !strings.Contains(ev.Text, "where") {
				fmt.Fprintf(&builder, "I'm AWS Cost Anomaly Detection Reactor, If you need to running infomation, please mention me with `where`")
			} else {
				fmt.Fprintf(&builder, "AWS Cost Anomaly Detection Reactor running infomation\n")
				if h.awsAccountID != "" {
					fmt.Fprintf(&builder, "- aws_account_id: %s\n", h.awsAccountID)
					fmt.Fprintf(&builder, "- region: %s\n", os.Getenv("AWS_REGION"))
				}
				if lambdacontext.FunctionName != "" {
					fmt.Fprintf(&builder, "- lambda_function_name: %s\n", lambdacontext.FunctionName)
					fmt.Fprintf(&builder, "- lambda_function_version: %s\n", lambdacontext.FunctionVersion)
				}
				if hostname, err := os.Hostname(); err == nil {
					fmt.Fprintf(&builder, "- hostname: %s\n", hostname)
				}
			}
			h.logger.Info("post message", "text", builder.String())
			_, _, err := h.client.PostMessage(ev.Channel, slack.MsgOptionText(builder.String(), false))
			if err != nil {
				h.logger.Error("failed to post message", "error", err)
			}
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) postAnomalyDetectedMessage(ctx context.Context, a Anomaly) error {
	data, err := h.newTemplateData(ctx, a)
	if err != nil {
		return fmt.Errorf("failed to create template data: %w", err)
	}
	opts, err := h.newDetectAnomalyMessageOptions(data)
	if err != nil {
		return fmt.Errorf("failed to create message: %w", err)
	}
	g := NewGraphGenerator(h.ce, h.org)
	graphs, err := g.Generate(ctx, a)
	if err != nil {
		return fmt.Errorf("failed to generate graphs: %w", err)
	}
	var posted bool
	var ts string
	if h.EnableDynamoDB() {
		msg, ok, err := h.GetAnomalySlackMessage(ctx, a.AnomalyID)
		if err != nil {
			h.logger.WarnContext(ctx, "failed to get anomaly slack message", "error", err)
		}
		if ok {
			posted = true
			ts = msg.SlackMessageTimestamp
			updateText := fmt.Sprintf("Update Total Impact `%f` to `%f`", msg.TotalImpact, a.Impact.TotalImpact)
			_, _, err = h.client.PostMessageContext(
				ctx, h.channel,
				slack.MsgOptionTS(ts),
				slack.MsgOptionText(updateText, false),
			)
			if err != nil {
				return fmt.Errorf("failed to post message: %w", err)
			}
			_, _, _, err = h.client.UpdateMessageContext(ctx, h.channel, ts, opts...)
			if err != nil {
				return fmt.Errorf("failed to update message: %w", err)
			}
		}
	}
	if !posted {
		_, ts, err = h.client.PostMessageContext(ctx, h.channel, opts...)
		if err != nil {
			return fmt.Errorf("failed to post message: %w", err)
		}
		if h.EnableDynamoDB() {
			if err := h.SaveAnomalySlackMessage(ctx, &AnomalySlackMessage{
				AnomalyID:             a.AnomalyID,
				SlackMessageTimestamp: ts,
				TotalImpact:           a.Impact.TotalImpact,
			}); err != nil {
				h.logger.WarnContext(ctx, "failed to save anomaly slack message", "error", err, "anomaly_id", a.AnomalyID)
			}
		}
	}
	h.logger.Info("post anomaly detected message", "anomaly_id", a.AnomalyID, "thread_ts", ts)
	for i, g := range graphs {
		name := fmt.Sprintf("anomaly-%s-root-cause%d.png", a.AnomalyID, i+1)
		file, err := h.client.UploadFileV2Context(ctx, slack.UploadFileV2Parameters{
			Reader:          g.r,
			Filename:        name,
			FileSize:        int(g.size), // v2 API requires file size
			Channel:         h.channel,
			ThreadTimestamp: ts,
		})
		if err != nil {
			return fmt.Errorf("failed to upload file: %w", err)
		}
		h.logger.Info("upload file", "file_id", file.ID, "file_name", name)
	}
	return nil
}

func (h *Handler) ProvideFeedback(ctx context.Context, annomalyID string, actionID string) error {
	var feedbackType types.AnomalyFeedbackType
	switch actionID {
	case actionsYesID:
		feedbackType = types.AnomalyFeedbackTypeYes
	case actionsNoID:
		feedbackType = types.AnomalyFeedbackTypeNo
	case actionsPlanedActivityID:
		feedbackType = types.AnomalyFeedbackTypePlannedActivity
	default:
		return fmt.Errorf("invalid action id: %s", actionID)
	}
	_, err := h.ce.ProvideAnomalyFeedback(ctx, &costexplorer.ProvideAnomalyFeedbackInput{
		AnomalyId: aws.String(annomalyID),
		Feedback:  feedbackType,
	})
	if err != nil {
		return err
	}
	return nil
}
