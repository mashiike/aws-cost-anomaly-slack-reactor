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
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/gorilla/mux"
	"github.com/mashiike/canyon"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

type Handler struct {
	svc          *costExplorerService
	client       *slack.Client
	logger       *slog.Logger
	router       *mux.Router
	channel      string
	botUserID    string
	botID        string
	signalSecret string
	awsAccountID string
	tpl          *template.Template
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

	var botID, botUserID string
	client := slack.New(params.slackBotToken)
	if params.slackBotToken != "" {
		me, err := client.AuthTest()
		if err != nil {
			return nil, err
		}
		params.logger.Info("running sloack bot", "bot_id", me.BotID, "bot_user_id", me.UserID)
		botID = me.BotID
		botUserID = me.UserID
	} else {
		params.logger.Warn("slack bot token is not set, running anonymous mode")
	}
	svc, err := newCostExplorerService(ctx, params)
	if err != nil {
		return nil, err
	}
	router := mux.NewRouter()
	h := &Handler{
		svc:          svc,
		logger:       params.logger.With("component", "handler"),
		router:       router,
		client:       client,
		botID:        botID,
		channel:      params.slackChannel,
		botUserID:    botUserID,
		signalSecret: params.slackSignalSecret,
		awsAccountID: awsAccountID,
		tpl:          tpl,
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
	router.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if err := h.postAnomalyDetectedMessage(ctx, "d77d5f19-c994-418b-8864-70ebc37df972"); err != nil {
			h.logger.Error("failed to post anomaly detected message", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
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
	noticeBlockID           = "notice"
)

type templateData struct {
	AnomalyID                  string
	AnomalyTimeRange           string
	MonitorID                  string
	AccountID                  string
	Region                     string
	ActionsBlockID             string
	ActionsYesValue            string
	ActionsYesID               string
	ActionsNoValue             string
	ActionsNoID                string
	ActionsPlanedActivityValue string
	ActionsPlanedActivityID    string
	AnomalyStartDate           string
	AnomalyEndDate             string
	AnomalyScore               types.AnomalyScore
	Impact                     types.Impact
	RootCauses                 []types.RootCause
}

func (h *Handler) newTemplateData(ctx context.Context, anomalyID string) (templateData, error) {
	anomaly, err := h.svc.GetAnomaly(ctx, anomalyID)
	if err != nil {
		return templateData{}, fmt.Errorf("failed to get anomaly: %w", err)
	}
	var monitorID string
	arnObj, err := arn.Parse(*anomaly.MonitorArn)
	if err != nil {
		return templateData{}, fmt.Errorf("failed to parse monitor arn: %w", err)
	}
	monitorID = strings.TrimPrefix(arnObj.Resource, "anomalymonitor/")
	data := templateData{
		AnomalyID:      *anomaly.AnomalyId,
		MonitorID:      monitorID,
		AccountID:      h.awsAccountID,
		ActionsBlockID: actionsBlockID,
		ActionsYesValue: url.Values{
			"anomaly_id": []string{*anomaly.AnomalyId},
			"action":     []string{string(types.AnomalyFeedbackTypeYes)},
		}.Encode(),
		ActionsYesID: actionsYesID,
		ActionsNoValue: url.Values{
			"anomaly_id": []string{*anomaly.AnomalyId},
			"action":     []string{string(types.AnomalyFeedbackTypeNo)},
		}.Encode(),
		ActionsNoID: actionsNoID,
		ActionsPlanedActivityValue: url.Values{
			"anomaly_id": []string{*anomaly.AnomalyId},
			"action":     []string{string(types.AnomalyFeedbackTypePlannedActivity)},
		}.Encode(),
		ActionsPlanedActivityID: actionsPlanedActivityID,
		RootCauses:              anomaly.RootCauses,
	}
	if anomaly.AnomalyScore != nil {
		data.AnomalyScore = *anomaly.AnomalyScore
	}
	if anomaly.Impact != nil {
		data.Impact = *anomaly.Impact
	}
	today := time.Now().Format("2006-01-02")
	startDate := today
	if anomaly.AnomalyStartDate != nil && *anomaly.AnomalyStartDate != "" {
		startDate = *anomaly.AnomalyStartDate
		data.AnomalyStartDate = startDate
	}
	endDate := today
	if anomaly.AnomalyEndDate != nil && *anomaly.AnomalyEndDate != "" {
		endDate = *anomaly.AnomalyEndDate
		data.AnomalyEndDate = endDate
	}
	if startDate == endDate {
		data.AnomalyTimeRange = startDate
	} else {
		data.AnomalyTimeRange = fmt.Sprintf("%s - %s", startDate, endDate)
	}
	return data, nil
}

func (h *Handler) newDetectAnomalyMessageOptions(data templateData) ([]slack.MsgOption, error) {
	var buf bytes.Buffer
	if err := h.tpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}
	msg := slack.NewPostMessageParameters()
	bs := buf.Bytes()
	dec := json.NewDecoder(bytes.NewReader(bs))
	if err := dec.Decode(&msg); err != nil {
		return nil, fmt.Errorf("failed to decode template: %w", err)
	}
	dec = json.NewDecoder(bytes.NewReader(bs))
	opts := []slack.MsgOption{slack.MsgOptionPostMessageParameters(msg)}
	var m map[string]json.RawMessage
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("failed to extra message: %w", err)
	}
	if _, ok := m["attachments"]; ok {
		var attachments []slack.Attachment
		if err := json.Unmarshal(m["attachments"], &attachments); err != nil {
			return nil, fmt.Errorf("failed to unmarshal attachments: %w", err)
		}
		opts = append(opts, slack.MsgOptionAttachments(attachments...))
	}
	if _, ok := m["blocks"]; ok {
		var blocks slack.Blocks
		if err := json.Unmarshal(m["blocks"], &blocks); err != nil {
			return nil, fmt.Errorf("failed to unmarshal blocks: %w", err)
		}
		opts = append(opts, slack.MsgOptionBlocks(blocks.BlockSet...))
	}
	return opts, nil
}

func (h *Handler) handleAmazonSNS(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug("start handle amazon sns")
	var n httpNotification
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&n); err != nil {
		h.logger.Error("failed to decode body", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	h.logger.Info("handle amazon sns http notification", "type", n.Type, "topic_arn", n.TopicArn, "subject", n.Subject)
	ctx := r.Context()
	arnObj, err := arn.Parse(n.TopicArn)
	if err != nil {
		h.logger.Error("failed to parse arn", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	switch n.Type {
	case "SubscriptionConfirmation":
		h.logger.Info("subscription confirmation", "subscribe_url", n.SubscribeURL)

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
		// TODO: post anomaly detected message
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
	msg := payload.Message
	var noticeBlockIndex int = -1
	for i, b := range msg.Blocks.BlockSet {
		section, ok := b.(*slack.SectionBlock)
		if !ok {
			continue
		}
		if section.BlockID == noticeBlockID {
			noticeBlockIndex = i
			break
		}
	}
	if noticeBlockIndex == -1 {
		noticeBlockIndex = len(msg.Blocks.BlockSet)
		msg.Blocks.BlockSet = append(msg.Blocks.BlockSet, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, "[notice block]", false, false), nil, nil))
	}

	if canyon.Used(r) && !canyon.IsWorker(r) {
		msgID, err := canyon.SendToWorker(r, nil)
		if err != nil {
			h.logger.Error("failed to send to worker", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		h.logger.Info("send process intaracitve message request to worker", "msg_id", msgID)
		block := slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf("Processing feedback action request... (msg_id: %s)", msgID), false, false), nil, nil)
		block.BlockID = noticeBlockID
		msg.Blocks.BlockSet[noticeBlockIndex] = block
		w.WriteHeader(http.StatusOK)
		return
	}

	json.NewEncoder(os.Stdout).Encode(payload)
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
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	anomaryID := v.Get("anomaly_id")
	h.logger.Info("provide feedback action", "anomaly_id", anomaryID, "action_id", action.ActionID, "user_id", actionUser.ID)
	stauts := http.StatusOK
	if err := h.svc.ProvideFeedback(r.Context(), anomaryID, action.ActionID); err != nil {
		h.logger.Error("failed to provide feedback", "error", err)
		block := slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf("[error] failed to provide feedback: %s", err), false, false), nil, nil)
		block.BlockID = noticeBlockID
		msg.Blocks.BlockSet[noticeBlockIndex] = block
		stauts = http.StatusInternalServerError
	} else {
		block := slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf("Feedback `%s` provided by user %s", action.Text.Text, actionUser.Name), false, false), nil, nil)
		block.BlockID = noticeBlockID
		msg.Blocks.BlockSet[noticeBlockIndex] = block
	}
	_, _, _, err = h.client.UpdateMessage(
		payload.Channel.ID,
		payload.Message.Timestamp,
		slack.MsgOptionBlocks(msg.Blocks.BlockSet...),
	)
	if err != nil {
		h.logger.Warn("failed to update message", "error", err)
	}
	w.WriteHeader(stauts)
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

func (h *Handler) postAnomalyDetectedMessage(ctx context.Context, anomalyID string) error {
	data, err := h.newTemplateData(ctx, anomalyID)
	if err != nil {
		return fmt.Errorf("failed to create template data: %w", err)
	}
	opts, err := h.newDetectAnomalyMessageOptions(data)
	if err != nil {
		return fmt.Errorf("failed to create message: %w", err)
	}
	_, ts, err := h.client.PostMessageContext(ctx, h.channel, opts...)
	if err != nil {
		return fmt.Errorf("failed to post message: %w", err)
	}
	readers, err := h.svc.GenerateGraphs(ctx, anomalyID)
	if err != nil {
		return fmt.Errorf("failed to generate graphs: %w", err)
	}
	for i, r := range readers {
		file, err := h.client.UploadFileContext(ctx, slack.FileUploadParameters{
			Reader:          r,
			Filename:        fmt.Sprintf("anomaly-%s-root-cause%d.png", anomalyID, i+1),
			Channels:        []string{h.channel},
			ThreadTimestamp: ts,
		})
		if err != nil {
			return fmt.Errorf("failed to upload file: %w", err)
		}
		h.logger.Info("upload file", "file_id", file.ID, "file_name", file.Name)
	}
	return nil
}
