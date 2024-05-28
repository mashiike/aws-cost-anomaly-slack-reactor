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

type Option func(*optionParams)

func WithAWSConfig(cfg *aws.Config) Option {
	return func(args *optionParams) {
		args.awsCfg = cfg
	}
}

func WithSlackBotToken(token string) Option {
	return func(args *optionParams) {
		args.slackBotToken = token
	}
}

func WithLogger(logger *slog.Logger) Option {
	return func(args *optionParams) {
		args.logger = logger
	}
}

func WithSlackChannel(channel string) Option {
	return func(args *optionParams) {
		args.slackChannel = channel
	}
}

func WithSlackSignalSecret(secret string) Option {
	return func(args *optionParams) {
		args.slackSignalSecret = secret
	}
}

func WithTemplate(template string) Option {
	return func(args *optionParams) {
		args.templateStr = template
	}
}

func WithNoErrorReport() Option {
	return func(args *optionParams) {
		args.noErrorReport = true
	}
}

func WithDynamoDBTableName(tableName string) Option {
	return func(args *optionParams) {
		args.dynamodbTableName = tableName
	}
}
