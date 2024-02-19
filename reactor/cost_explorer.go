package reactor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
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
	"gonum.org/v1/plot/vg"
)

type costExplorerService struct {
	client       *costexplorer.Client
	cacheAnomaly map[string]types.Anomaly
	nextEndDate  time.Time
	allGet       bool
	dateInterval *types.AnomalyDateInterval
	logger       *slog.Logger
}

var (
	ErrAnomalyNotFound = errors.New("anomaly not found")
)

func newCostExplorerService(ctx context.Context, params *optionParams) (*costExplorerService, error) {
	client := costexplorer.NewFromConfig(*params.awsCfg)
	now := time.Now()
	costExplorerService := &costExplorerService{
		client:       client,
		cacheAnomaly: make(map[string]types.Anomaly),
		nextEndDate:  now.AddDate(0, 0, -28),
		dateInterval: &types.AnomalyDateInterval{
			StartDate: aws.String(now.AddDate(0, 0, -28).Format("2006-01-02")),
			EndDate:   aws.String(now.Format("2006-01-02")),
		},
		logger: params.logger.With("component", "cost_explorer"),
	}
	return costExplorerService, nil
}

func (r *costExplorerService) GetAnomaly(ctx context.Context, anomalyID string) (types.Anomaly, error) {
	r.logger.Info("get anomaly", "anomaly_id", anomalyID)
	if a, ok := r.cacheAnomaly[anomalyID]; ok {
		return a, nil
	}
	if r.allGet {
		return types.Anomaly{}, ErrAnomalyNotFound
	}
	for {
		select {
		case <-ctx.Done():
			return types.Anomaly{}, ctx.Err()
		default:
		}
		r.logger.Debug("get anomalies", "start_date", *r.dateInterval.StartDate, "end_date", *r.dateInterval.EndDate)
		anomaly, err := getAnomalies(ctx, r.client, r.dateInterval)
		if err != nil {
			return types.Anomaly{}, err
		}
		startDate := r.nextEndDate.AddDate(0, 0, -28)
		r.dateInterval.StartDate = aws.String(startDate.Format("2006-01-02"))
		r.dateInterval.EndDate = aws.String(r.nextEndDate.Format("2006-01-02"))
		r.nextEndDate = startDate
		if len(anomaly) == 0 {
			r.allGet = true
			return types.Anomaly{}, ErrAnomalyNotFound
		}
		for _, a := range anomaly {
			r.cacheAnomaly[*a.AnomalyId] = a
		}
		if _, ok := r.cacheAnomaly[anomalyID]; ok {
			return r.cacheAnomaly[anomalyID], nil
		}
	}
}

func getAnomalies(ctx context.Context, client costexplorerx.GetAnomaliesAPIClient, dateInterval *types.AnomalyDateInterval) ([]types.Anomaly, error) {
	ret := make([]types.Anomaly, 0)
	p := costexplorerx.NewGetAnomaliesPaginator(client, &costexplorer.GetAnomaliesInput{
		DateInterval: dateInterval,
	})
	for p.HasMorePages() {
		out, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		ret = append(ret, out.Anomalies...)
	}
	return ret, nil
}

func (r *costExplorerService) GenerateGraphs(ctx context.Context, anomalyID string) ([]io.Reader, error) {
	anomaly, err := r.GetAnomaly(ctx, anomalyID)
	if err != nil {
		return nil, fmt.Errorf("failed to get anomaly: %w", err)
	}
	anomalyStartAt, err := time.Parse(time.RFC3339, *anomaly.AnomalyStartDate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse anomaly start date: %w", err)
	}
	anomalyEndAt, err := time.Parse(time.RFC3339, *anomaly.AnomalyEndDate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse anomaly end date: %w", err)
	}
	graphs := make([]io.Reader, 0, len(anomaly.RootCauses))
	for _, c := range anomaly.RootCauses {
		w, err := r.GenerateGraph(ctx, anomalyStartAt.AddDate(0, 0, -8), anomalyEndAt.AddDate(0, 0, 8), c)
		if err != nil {
			return nil, fmt.Errorf("failed to generate graph: %w", err)
		}
		var buf bytes.Buffer
		if _, err := w.WriteTo(&buf); err != nil {
			return nil, fmt.Errorf("failed to write graph: %w", err)
		}
		graphs = append(graphs, &buf)
	}
	return graphs, nil
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

func (r *costExplorerService) GenerateGraph(ctx context.Context, startAt, endAt time.Time, c types.RootCause) (io.WriterTo, error) {
	costLabel := []string{}
	andExpr := []types.Expression{
		{
			Dimensions: &types.DimensionValues{
				Key:    types.DimensionRecordType,
				Values: []string{"Usage"},
			},
		},
	}
	if c.LinkedAccount != nil {
		andExpr = append(andExpr, types.Expression{
			Dimensions: &types.DimensionValues{
				Key:    types.DimensionLinkedAccount,
				Values: []string{*c.LinkedAccount},
			},
		})
		costLabel = append(costLabel, fmt.Sprintf("%s(%s)", *c.LinkedAccountName, *c.LinkedAccount))
	}
	if c.Region != nil {
		andExpr = append(andExpr, types.Expression{
			Dimensions: &types.DimensionValues{
				Key:    types.DimensionRegion,
				Values: []string{*c.Region},
			},
		})
		costLabel = append(costLabel, *c.Region)
	}
	if c.Service != nil {
		andExpr = append(andExpr, types.Expression{
			Dimensions: &types.DimensionValues{
				Key:    types.DimensionService,
				Values: []string{*c.Service},
			},
		})
		costLabel = append(costLabel, *c.Service)
	}
	if c.UsageType != nil {
		andExpr = append(andExpr, types.Expression{
			Dimensions: &types.DimensionValues{
				Key:    types.DimensionUsageType,
				Values: []string{*c.UsageType},
			},
		})
		costLabel = append(costLabel, *c.UsageType)
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
	paginator := costexplorerx.NewGetCostAndUsagePaginator(r.client, input)
	costs := make(dateCosts, 0, 28)
	dates := make([]string, 0, 28)
	unit := ""
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
			costs = append(costs, dateCost{Date: date, Cost: cost})
			dates = append(dates, date.Format("2006-01-02"))
		}
	}
	title := strings.Join(costLabel, ",")
	r.logger.Info("generate graph", "title", title, "start_at", startAt, "end_at", endAt)
	p := plot.New()
	p.Title.Text = title
	p.X.Label.Text = "Date"
	p.X.Tick.Marker = dateTicker{Dates: dates}
	p.Y.Label.Text = fmt.Sprintf("Cost (%s)", unit)

	bars, err := plotter.NewBarChart(costs, vg.Points(20))
	if err != nil {
		return nil, err
	}
	bars.LineStyle.Width = vg.Length(0)
	bars.Color = color.RGBA{R: 0, G: 128, B: 255, A: 255}
	p.Add(bars)

	w, err := p.WriterTo(10*vg.Inch, 4*vg.Inch, "png")
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (r *costExplorerService) ProvideFeedback(ctx context.Context, anomalyID string, actionID string) error {
	r.logger.Info("provide feedback", "action_id", actionID)
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
	_, err := r.client.ProvideAnomalyFeedback(ctx, &costexplorer.ProvideAnomalyFeedbackInput{
		AnomalyId: aws.String(anomalyID),
		Feedback:  feedbackType,
	})
	if err != nil {
		return err
	}
	return nil
}
