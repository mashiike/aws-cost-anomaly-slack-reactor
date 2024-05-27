package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"github.com/fatih/color"
	"github.com/fujiwara/ridge"
	"github.com/handlename/ssmwrap"
	"github.com/ken39arg/go-flagx"
	"github.com/mashiike/aws-cost-anomaly-slack-reactor/reactor"
	"github.com/mashiike/canyon"
	"github.com/mashiike/slogutils"
)

func main() {
	if ssmWrapPaths := os.Getenv("SSMWRAP_PATHS"); ssmWrapPaths != "" {
		err := ssmwrap.Export(ssmwrap.ExportOptions{
			Recursive: true,
			Paths:     strings.Split(ssmWrapPaths, ","),
			Retries:   3,
		})
		if err != nil {
			log.Fatalf("failed to export SSM parameters: %v", err)
		}
	}
	if ssmWarpNames := os.Getenv("SSMWRAP_NAMES"); ssmWarpNames != "" {
		err := ssmwrap.Export(ssmwrap.ExportOptions{
			Recursive: true,
			Names:     strings.Split(ssmWarpNames, ","),
			Retries:   3,
		})
		if err != nil {
			log.Fatalf("failed to export SSM parameters: %v", err)
		}
	}
	var (
		logLevel          string
		address           string
		prefix            string
		sqsQueueName      string
		dynamodbTableName string
	)
	flag.StringVar(&logLevel, "log-level", "info", "log level")
	flag.StringVar(&address, "address", ":8080", "listen address")
	flag.StringVar(&prefix, "prefix", "/", "path prefix")
	flag.StringVar(&sqsQueueName, "sqs-queue-name", "", "SQS queue name")
	flag.StringVar(&dynamodbTableName, "dynamodb-table-name", "", "DynamoDB table name")
	flag.VisitAll(flagx.EnvToFlag)
	flag.Parse()
	var minLevel slog.Level
	switch strings.ToLower(logLevel) {
	case "debug":
		minLevel = slog.LevelDebug
	case "info":
		minLevel = slog.LevelInfo
	case "warn":
		minLevel = slog.LevelWarn
	case "error":
		minLevel = slog.LevelError
	default:
		log.Fatalf("invalid log level: %s", logLevel)
	}
	middleware := slogutils.NewMiddleware(
		slog.NewJSONHandler,
		slogutils.MiddlewareOptions{
			ModifierFuncs: map[slog.Level]slogutils.ModifierFunc{
				slog.LevelDebug: slogutils.Color(color.FgBlack),
				slog.LevelInfo:  nil,
				slog.LevelWarn:  slogutils.Color(color.FgYellow),
				slog.LevelError: slogutils.Color(color.FgRed, color.Bold),
			},
			Writer: os.Stderr,
			HandlerOptions: &slog.HandlerOptions{
				Level: minLevel,
			},
		},
	)
	slog.SetDefault(slog.New(middleware))
	slog.Info("setup logger", "level", minLevel)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var opts []reactor.Option
	if dynamodbTableName != "" {
		opts = append(opts, reactor.WithDynamoDBTableName(dynamodbTableName))
	}
	h, err := reactor.New(ctx, opts...)
	if err != nil {
		log.Fatal(err)
	}
	if sqsQueueName == "" {
		ridge.RunWithContext(ctx, address, prefix, h)
	} else {
		canyon.RunWithContext(ctx, sqsQueueName, h,
			canyon.WithServerAddress(address, prefix),
			canyon.WithCanyonEnv("CANYON_"),
		)
	}
}
