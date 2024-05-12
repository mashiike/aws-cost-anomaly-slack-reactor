package reactor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/mashiike/aws-cost-anomaly-slack-reactor/internal/costexplorerx"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
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

type GraphGenerator struct {
	client costexplorerx.GetCostAndUsageAPIClient
}

func NewGraphGenerator(client costexplorerx.GetCostAndUsageAPIClient) *GraphGenerator {
	return &GraphGenerator{
		client: client,
	}
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

func firstDayOfNextMonth() time.Time {
	now := time.Now()
	nextMonth := now.Month() + 1
	year := now.Year()

	if nextMonth > 12 {
		nextMonth = 1
		year++
	}
	return time.Date(year, nextMonth, 1, 0, 0, 0, 0, time.Local)
}

func lastDayOfThisMonth() time.Time {
	return firstDayOfNextMonth().Add(-time.Second)
}

func (g *GraphGenerator) generate(ctx context.Context, startAt, endAt time.Time, c RootCause) (io.WriterTo, error) {
	costLabel := []string{}
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
	// for ValidationException: end date past the beginning of next month
	if endAt.After(lastDayOfThisMonth()) {
		endAt = lastDayOfThisMonth()
	}
	input := &costexplorer.GetCostAndUsageInput{
		Granularity: types.GranularityDaily,
		TimePeriod: &types.DateInterval{
			Start: aws.String(startAt.Format("2006-01-02")),
			End:   aws.String(endAt.Format("2006-01-02")),
		},
		Filter: &types.Expression{
			And: andExpr,
		},
		GroupBy: []types.GroupDefinition{},
		Metrics: []string{"NET_UNBLENDED_COST"},
	}
	slog.Info("get cost and usage", "start_at", startAt, "end_at", endAt, "input", input)
	paginator := costexplorerx.NewGetCostAndUsagePaginator(g.client, input)
	unit := ""
	graph := NewCostGraph()
	for paginator.HasMorePages() {
		out, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get cost and usage: %w", err)
		}
		for _, data := range out.ResultsByTime {
			date, err := time.Parse("2006-01-02", *data.TimePeriod.Start)
			if err != nil {
				return nil, fmt.Errorf("failed to parse point date: %w", err)
			}
			netUnblendedCost, ok := data.Total["NetUnblendedCost"]
			if !ok {
				return nil, errors.New("NetUnblendedCost not found")
			}
			cost, err := strconv.ParseFloat(*netUnblendedCost.Amount, 64)
			if err != nil {
				return nil, err
			}
			unit = *netUnblendedCost.Unit
			graph.AddDataPoint(date, cost, "NetUnblendedCost")
		}
	}
	title := strings.Join(costLabel, ",")
	slog.InfoContext(ctx, "generate graph", "title", title, "start_at", startAt, "end_at", endAt)
	w, err := graph.WriteTo(title, fmt.Sprintf("Cost (%s)", unit))
	if err != nil {
		return nil, err
	}
	return w, nil
}

type dateCost struct {
	Date time.Time
	Cost float64
}

type dateCosts []dateCost

var _ plotter.Valuer = dateCosts(nil)

func (dc dateCosts) Len() int {
	return len(dc)
}

func (dc dateCosts) Value(i int) float64 {
	return dc[i].Cost
}

type dateTicker struct {
	Dates []string
}

func (dt dateTicker) Ticks(min, max float64) []plot.Tick {
	maxLabels := 8
	interval := int(math.Ceil(float64(len(dt.Dates)) / float64(maxLabels)))
	var ticks []plot.Tick
	for i, date := range dt.Dates {
		if float64(i) >= min && float64(i) <= max {
			tick := plot.Tick{
				Value: float64(i),
				Label: date,
			}
			if int(float64(i)-min)%interval != 0 {
				tick.Label = ""
			}
			ticks = append(ticks, tick)
		}
	}
	return ticks
}
