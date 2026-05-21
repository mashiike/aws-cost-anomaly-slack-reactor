package reactor

import (
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type optionParams struct {
	logger            *slog.Logger
	awsCfg            *aws.Config
	slackBotToken     string
	slackChannel      string
	slackSignalSecret string
	templateStr       string
	dynamodbTableName string
	noErrorReport     bool
}

// Option configures a Handler created by New.
type Option func(*optionParams)

// WithAWSConfig sets the AWS SDK config used by the Handler.
func WithAWSConfig(cfg *aws.Config) Option {
	return func(args *optionParams) {
		args.awsCfg = cfg
	}
}

// WithSlackBotToken sets the Slack bot token used by the Handler.
func WithSlackBotToken(token string) Option {
	return func(args *optionParams) {
		args.slackBotToken = token
	}
}

// WithLogger sets the slog.Logger used by the Handler.
func WithLogger(logger *slog.Logger) Option {
	return func(args *optionParams) {
		args.logger = logger
	}
}

// WithSlackChannel sets the Slack channel the Handler posts messages to.
func WithSlackChannel(channel string) Option {
	return func(args *optionParams) {
		args.slackChannel = channel
	}
}

// WithSlackSignalSecret sets the Slack signing secret used to verify incoming
// Slack events.
func WithSlackSignalSecret(secret string) Option {
	return func(args *optionParams) {
		args.slackSignalSecret = secret
	}
}

// WithTemplate sets the message template used by the Handler.
func WithTemplate(template string) Option {
	return func(args *optionParams) {
		args.templateStr = template
	}
}

// WithNoErrorReport disables posting handler errors back to Slack.
func WithNoErrorReport() Option {
	return func(args *optionParams) {
		args.noErrorReport = true
	}
}

// WithDynamoDBTableName enables DynamoDB-backed state and sets the table name.
func WithDynamoDBTableName(tableName string) Option {
	return func(args *optionParams) {
		args.dynamodbTableName = tableName
	}
}
