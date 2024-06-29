package reactor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Songmu/flextime"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/mashiike/aws-cost-anomaly-slack-reactor/internal/costexplorerx"
)

type Anomaly struct {
	AccountID          string        `json:"accountId"`
	AnomalyDetailsLink string        `json:"anomalyDetailsLink"`
	AnomalyEndDate     time.Time     `json:"anomalyEndDate"`
	AnomalyID          string        `json:"anomalyId"`
	AnomalyScore       AnomalyScore  `json:"anomalyScore"`
	AnomalyStartDate   time.Time     `json:"anomalyStartDate"`
	DimensionalValue   string        `json:"dimensionalValue"`
	Impact             AnomalyImpact `json:"impact"`
	MonitorArn         string        `json:"monitorArn"`
	RootCauses         []RootCause   `json:"rootCauses"`
	SubscriptionID     string        `json:"subscriptionId"`
	SubscriptionName   string        `json:"subscriptionName"`
}

type AnomalyScore struct {
	CurrentScore float64 `json:"currentScore"`
	MaxScore     float64 `json:"maxScore"`
}

type AnomalyImpact struct {
	MaxImpact             float64 `json:"maxImpact"`
	TotalActualSpend      float64 `json:"totalActualSpend"`
	TotalExpectedSpend    float64 `json:"totalExpectedSpend"`
	TotalImpact           float64 `json:"totalImpact"`
	TotalImpactPercentage float64 `json:"totalImpactPercentage"`
}

type RootCause struct {
	LinkedAccount     string `json:"linkedAccount"`
	LinkedAccountName string `json:"linkedAccountName"`
	Region            string `json:"region"`
	Service           string `json:"service"`
	UsageType         string `json:"usageType"`
}

type Graph struct {
	r    io.Reader
	size int64
}

type DescribeAccountAPIClient interface {
	DescribeAccount(ctx context.Context, input *organizations.DescribeAccountInput, optFns ...func(*organizations.Options)) (*organizations.DescribeAccountOutput, error)
}

type GraphGenerator struct {
	client                     costexplorerx.GetCostAndUsageAPIClient
	org                        DescribeAccountAPIClient
	cacheDescribeAccountOutput map[string]*organizations.DescribeAccountOutput
	cacheDescribeAccountError  map[string]error
	cacheDescribeAccountMu     sync.Mutex
	cacheDescribeAccountExpire map[string]time.Time
}

func NewGraphGenerator(client costexplorerx.GetCostAndUsageAPIClient, org DescribeAccountAPIClient) *GraphGenerator {
	return &GraphGenerator{
		client:                     client,
		org:                        org,
		cacheDescribeAccountOutput: make(map[string]*organizations.DescribeAccountOutput),
		cacheDescribeAccountError:  make(map[string]error),
		cacheDescribeAccountExpire: make(map[string]time.Time),
	}
}

func (g *GraphGenerator) describeAccount(ctx context.Context, accountID string) (*organizations.DescribeAccountOutput, error) {
	g.cacheDescribeAccountMu.Lock()
	defer g.cacheDescribeAccountMu.Unlock()
	if expire, ok := g.cacheDescribeAccountExpire[accountID]; ok && time.Now().Before(expire) {
		return g.cacheDescribeAccountOutput[accountID], g.cacheDescribeAccountError[accountID]
	}
	out, err := g.org.DescribeAccount(ctx, &organizations.DescribeAccountInput{AccountId: aws.String(accountID)})
	g.cacheDescribeAccountExpire[accountID] = time.Now().Add(1 * time.Hour)
	if err != nil {
		g.cacheDescribeAccountError[accountID] = err
		return nil, err
	}
	g.cacheDescribeAccountOutput[accountID] = out
	return out, nil
}

func (g *GraphGenerator) Generate(ctx context.Context, anomaly Anomaly) ([]*Graph, error) {
	graphs := make([]*Graph, 0, len(anomaly.RootCauses))
	for _, c := range anomaly.RootCauses {
		w, err := g.generate(ctx, anomaly.AnomalyStartDate.AddDate(0, 0, -8), anomaly.AnomalyEndDate.AddDate(0, 0, 8), c)
		if err != nil {
			return nil, fmt.Errorf("failed to generate graph: %w", err)
		}
		var buf bytes.Buffer
		n, err := w.WriteTo(&buf)
		if err != nil {
			return nil, fmt.Errorf("failed to write graph: %w", err)
		}
		graphs = append(graphs, &Graph{r: &buf, size: n})
	}
	return graphs, nil
}

func (g *GraphGenerator) generate(ctx context.Context, startAt, endAt time.Time, c RootCause) (io.WriterTo, error) {
	graph := NewCostGraph()
	title, unit, err := g.renderGraph(ctx, graph, startAt, endAt, c, "", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to render graph: %w", err)
	}
	if g.IsSavingsPlanApplied(c) && c.LinkedAccount != "" {
		// render graph without SavingsPlan
		_, _, err = g.renderGraph(ctx, graph, startAt, endAt, c, " (without SavingsPlan)", []types.Expression{
			{
				Not: &types.Expression{
					Dimensions: &types.DimensionValues{
						Key:    types.DimensionRecordType,
						Values: []string{"SavingsPlanNegation"},
					},
				},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to render graph without SavingsPlan: %w", err)
		}
		graph.EnableStack = false
	}
	slog.InfoContext(ctx, "generate graph", "title", title, "start_at", startAt, "end_at", endAt)
	w, err := graph.WriteTo(title, fmt.Sprintf("Cost (%s)", unit))
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (g *GraphGenerator) IsSavingsPlanApplied(c RootCause) bool {
	if strings.Contains(c.Service, "Amazon Elastic Compute Cloud") {
		return true
	}
	if strings.Contains(c.Service, "Amazon Elastic Container Service") {
		return true
	}
	if strings.Contains(c.Service, "AWS Lambda") {
		return true
	}
	return false
}

func generateTimePeriods(startAt time.Time, endAt time.Time) []*types.DateInterval {
	// group by month, start=2024-01-29, end=2024-02-04 => [2024-01-29, 2024-01-31], [2024-02-01, 2024-02-04]
	today := flextime.Now().Truncate(time.Hour * 24)
	timePeriods := []*types.DateInterval{}
	current := startAt
	next := current.AddDate(0, 0, 1)
	for next.Before(endAt) {
		// if month is different, append timePeriod
		if current.Year() != next.Year() || current.Month() != next.Month() {
			timePeriods = append(timePeriods, &types.DateInterval{
				Start: aws.String(current.Format("2006-01-02")),
				End:   aws.String(next.Format("2006-01-02")),
			})
			current = next
		}
		next = next.AddDate(0, 0, 1)
	}
	if current.Compare(today) <= 0 {
		timePeriods = append(timePeriods, &types.DateInterval{
			Start: aws.String(current.Format("2006-01-02")),
			End:   aws.String(endAt.AddDate(0, 0, 1).Format("2006-01-02")),
		})
	}
	return timePeriods
}

func (g *GraphGenerator) renderGraph(ctx context.Context, graph *CostGraph, startAt, endAt time.Time, c RootCause, extraLabel string, extraFilters []types.Expression) (string, string, error) {
	costLabel := []string{}
	groupBy := []types.GroupDefinition{}
	andExpr := []types.Expression{
		{
			Dimensions: &types.DimensionValues{
				Key:    types.DimensionRecordType,
				Values: []string{"Usage"},
			},
		},
	}
	if c.LinkedAccount != "" {
		andExpr = append(andExpr, types.Expression{
			Dimensions: &types.DimensionValues{
				Key:    types.DimensionLinkedAccount,
				Values: []string{c.LinkedAccount},
			},
		})
		costLabel = append(costLabel, fmt.Sprintf("%s(%s)", c.LinkedAccountName, c.LinkedAccount))
	} else {
		groupBy = append(groupBy, types.GroupDefinition{
			Type: types.GroupDefinitionTypeDimension,
			Key:  aws.String(string(types.DimensionLinkedAccount)),
		})
	}
	if c.Region != "" {
		andExpr = append(andExpr, types.Expression{
			Dimensions: &types.DimensionValues{
				Key:    types.DimensionRegion,
				Values: []string{c.Region},
			},
		})
		costLabel = append(costLabel, c.Region)
	}
	if c.Service != "" {
		andExpr = append(andExpr, types.Expression{
			Dimensions: &types.DimensionValues{
				Key:    types.DimensionService,
				Values: []string{c.Service},
			},
		})
		costLabel = append(costLabel, c.Service)
	}
	if c.UsageType != "" {
		andExpr = append(andExpr, types.Expression{
			Dimensions: &types.DimensionValues{
				Key:    types.DimensionUsageType,
				Values: []string{c.UsageType},
			},
		})
		costLabel = append(costLabel, c.UsageType)
	}
	andExpr = append(andExpr, extraFilters...)
	input := &costexplorer.GetCostAndUsageInput{
		Granularity: types.GranularityDaily,
		Filter: &types.Expression{
			And: andExpr,
		},
		GroupBy: groupBy,
		Metrics: []string{"NET_UNBLENDED_COST"},
	}
	slog.Info("get cost and usage", "start_at", startAt, "end_at", endAt, "input", input)
	timePeriods := generateTimePeriods(startAt, endAt)
	slog.Debug("generate time periods", "start_at", startAt, "end_at", endAt, "time_periods", timePeriods)
	unit := ""
	for _, tp := range timePeriods {
		input.TimePeriod = tp
		paginator := costexplorerx.NewGetCostAndUsagePaginator(g.client, input)
		for paginator.HasMorePages() {
			out, err := paginator.NextPage(ctx)
			if err != nil {
				return "", "", fmt.Errorf("failed to get cost and usage[%s~%s]: %w", *tp.Start, *tp.End, err)
			}
			for _, data := range out.ResultsByTime {
				date, err := time.Parse("2006-01-02", *data.TimePeriod.Start)
				if err != nil {
					return "", "", fmt.Errorf("failed to parse point date: %w", err)
				}
				if len(data.Groups) == 0 {
					netUnblendedCost, ok := data.Total["NetUnblendedCost"]
					if !ok {
						return "", "", errors.New("NetUnblendedCost not found")
					}
					cost, err := strconv.ParseFloat(*netUnblendedCost.Amount, 64)
					if err != nil {
						return "", "", err
					}
					unit = *netUnblendedCost.Unit
					graph.AddDataPoint(date, cost, "NetUnblendedCost"+extraLabel)
				} else {
					for _, group := range data.Groups {
						var groupLabels []string
						for keyIndex, v := range group.Keys {
							k := *out.GroupDefinitions[keyIndex].Key
							if k != "LINKED_ACCOUNT" {
								groupLabels = append(groupLabels, v)
								continue
							}
							desc, err := g.describeAccount(ctx, v)
							if err != nil {
								slog.Warn("failed to describe account", "account_id", v, "error", err)
								groupLabels = append(groupLabels, v)
							} else {
								groupLabels = append(groupLabels, fmt.Sprintf("%s (%s)", *desc.Account.Name, v))
							}
						}
						l := "(unknown)"
						if len(groupLabels) > 0 {
							l = strings.Join(groupLabels, ",")
						}
						netUnblendedCost, ok := group.Metrics["NetUnblendedCost"]
						if !ok {
							return "", "", errors.New("NetUnblendedCost not found")
						}
						cost, err := strconv.ParseFloat(*netUnblendedCost.Amount, 64)
						if err != nil {
							return "", "", err
						}
						unit = *netUnblendedCost.Unit
						graph.AddDataPoint(date, cost, l+extraLabel)
					}
				}
			}
		}
	}
	title := strings.Join(costLabel, ",")
	return title, unit, nil
}
