package reactor

import (
	"image/color"
	"io"
	"math"
	"sort"
	"sync"
	"time"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
)

type CostGraph struct {
	mu         sync.Mutex
	ticker     graphTicker
	dataPoints map[string]map[time.Time]float64
}

func NewCostGraph() *CostGraph {
	return &CostGraph{
		dataPoints: make(map[string]map[time.Time]float64),
		ticker: graphTicker{
			dates: make(map[time.Time]struct{}),
		},
	}
}

func (g *CostGraph) AddDataPoint(t time.Time, cost float64, legend string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.dataPoints[legend]; !ok {
		g.dataPoints[legend] = make(map[time.Time]float64)
	}
	g.dataPoints[legend][t] = cost
	g.ticker.AddDate(t)
}

const maxSeries = 10

var graphColors = []color.RGBA{
	{R: 51, G: 153, B: 255, A: 255},
	{R: 255, G: 102, B: 102, A: 255},
	{R: 46, G: 204, B: 113, A: 255},
	{R: 138, G: 43, B: 226, A: 255},
	{R: 255, G: 179, B: 0, A: 255},
	{R: 51, G: 12, B: 180, A: 255},
	{R: 0, G: 128, B: 0, A: 255},
	{R: 255, G: 102, B: 0, A: 255},
	{R: 126, G: 0, B: 185, A: 255},
	{R: 140, G: 81, B: 10, A: 255},
}

func (g *CostGraph) WriteTo(title string, yLabel string) (io.WriterTo, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// fill data points
	totals := make(map[string]float64)
	dataPoints := make(map[string]plotter.Values)
	legends := make([]string, 0, len(g.dataPoints))
	for legend, dp := range g.dataPoints {
		legends = append(legends, legend)
		dataPoints[legend] = make(plotter.Values, 0, g.ticker.Len())
		for _, date := range g.ticker.Dates() {
			if _, ok := dp[date]; !ok {
				dp[date] = 0
			}
			dataPoints[legend] = append(dataPoints[legend], dp[date])
			totals[legend] += dp[date]
		}
	}
	// sort by total cost
	sort.Slice(legends, func(i, j int) bool {
		totalI := totals[legends[i]]
		totalJ := totals[legends[j]]
		if totalI == totalJ {
			return legends[i] < legends[j]
		}
		return totalI > totalJ
	})
	// if too many series, combine into "others"
	if len(dataPoints) > maxSeries {
		legends = legends[:maxSeries-1]
		// combine others
		others := make(plotter.Values, g.ticker.Len())
		after := make(map[string]plotter.Values, maxSeries)
		isTop := func(ll string) bool {
			for _, l := range legends {
				if l == ll {
					return true
				}
			}
			return false
		}
		for l, dp := range dataPoints {
			if isTop(l) {
				after[l] = dp
			} else {
				for i, v := range dp {
					others[i] += v
				}
			}
		}
		after["Others"] = others
		dataPoints = after
		legends = append(legends, "Others")
	}
	// generate plot
	p := plot.New()
	p.Title.Text = title
	p.X.Label.Text = "Date"
	p.X.Tick.Marker = &g.ticker
	p.Y.Label.Text = yLabel
	colorIndex := 0
	var stack *plotter.BarChart
	for _, legend := range legends {
		dp := dataPoints[legend]
		bars, err := plotter.NewBarChart(dp, vg.Points(20))
		if err != nil {
			return nil, err
		}
		if stack != nil {
			bars.StackOn(stack)
		}
		stack = bars
		bars.LineStyle.Width = 0
		var c color.RGBA
		if colorIndex < len(graphColors) {
			c = graphColors[colorIndex]
		} else {
			c = color.RGBA{R: 0, G: 128, B: 255, A: 255}
		}
		colorIndex++
		bars.Color = c
		p.Add(bars)
		if len(dataPoints) > 1 {
			p.Legend.Add(legend, bars)
		}
	}
	p.Title.Padding = vg.Points(10)
	if len(dataPoints) > 1 {
		p.X.Max += 2
	}
	p.Legend.Top = true
	p.Legend.TextStyle.Font.Size = vg.Points(8)
	w, err := p.WriterTo(vg.Points(800), vg.Points(400), "png")
	if err != nil {
		return nil, err
	}
	return w, nil
}

type graphTicker struct {
	mu    sync.Mutex
	dates map[time.Time]struct{}
	cache []time.Time
}

func (t *graphTicker) AddDate(date time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.dates[date] = struct{}{}
	t.cache = nil
}

func (t *graphTicker) Dates() []time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cache != nil {
		return t.cache
	}
	dates := make([]time.Time, 0, len(t.dates))
	for date := range t.dates {
		dates = append(dates, date)
	}
	sort.Slice(dates, func(i, j int) bool {
		return dates[i].Before(dates[j])
	})
	t.cache = dates
	return dates
}

func (t *graphTicker) Len() int {
	return len(t.Dates())
}

const maxLabels = 8

func (t *graphTicker) Ticks(min, max float64) []plot.Tick {
	dates := t.Dates()
	interval := int(math.Ceil(float64(len(dates)) / float64(maxLabels)))
	var ticks []plot.Tick
	for i, date := range dates {
		if float64(i) >= min && float64(i) <= max {
			tick := plot.Tick{
				Value: float64(i),
				Label: date.Format("2006-01-02"),
			}
			if int(float64(i)-min)%interval != 0 {
				tick.Label = ""
			}
			ticks = append(ticks, tick)
		}
	}
	return ticks
}
