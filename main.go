package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"github.com/fatih/color"
	"github.com/fujiwara/ridge"
	"github.com/handlename/ssmwrap/v2"
	"github.com/ken39arg/go-flagx"
	"github.com/mashiike/aws-cost-anomaly-slack-reactor/reactor"
	"github.com/mashiike/canyon"
	"github.com/mashiike/slogutils"
)

func main() {
	if err := _main(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}
func _main() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	var ssmwrapExportRules []ssmwrap.ExportRule
	if ssmwrapPaths := os.Getenv("SSMWRAP_PATHS"); ssmwrapPaths != "" {
		for _, path := range strings.Split(ssmwrapPaths, ",") {
			path = strings.TrimSuffix(path, "/")
			ssmwrapExportRules = append(ssmwrapExportRules, ssmwrap.ExportRule{
				Path: path + "/**/*",
			})
		}
	}
	if ssmwarpNames := os.Getenv("SSMWRAP_NAMES"); ssmwarpNames != "" {
		for _, name := range strings.Split(ssmwarpNames, ",") {
			ssmwrapExportRules = append(ssmwrapExportRules, ssmwrap.ExportRule{
				Path: name,
			})
		}
	}
	if len(ssmwrapExportRules) > 0 {
		err := ssmwrap.Export(ctx, ssmwrapExportRules, ssmwrap.ExportOptions{
			Retries: 3,
		})
		if err != nil {
			return fmt.Errorf("failed to export SSM parameters: %w", err)
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
		err := canyon.RunWithContext(ctx, sqsQueueName, h,
			canyon.WithServerAddress(address, prefix),
			canyon.WithCanyonEnv("CANYON_"),
		)
		if err != nil {
			return fmt.Errorf("failed to run canyon: %w", err)
		}
	}
	return nil
}
