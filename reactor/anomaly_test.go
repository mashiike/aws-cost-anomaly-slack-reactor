package reactor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/sebdah/goldie/v2"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAnomalyMarshalJSON(t *testing.T) {
	bs, err := os.ReadFile("testdata/anomaly.json")
	require.NoError(t, err)
	var a Anomaly
	err = json.Unmarshal(bs, &a)
	require.NoError(t, err)
	require.NotEmpty(t, a)
	require.Equal(t, "https://console.aws.amazon.com/cost-management/home#/anomaly-detection/monitors/abcdef12-1234-4ea0-84cc-918a97d736ef/anomalies/12345678-abcd-ef12-3456-987654321a12", a.AnomalyDetailsLink)
	acutal, err := json.Marshal(a)
	require.NoError(t, err)
	require.JSONEq(t, string(bs), string(acutal))
}

type mockGetCostAndUsageAPIClient struct {
	mock.Mock
	t *testing.T
}

func (m *mockGetCostAndUsageAPIClient) GetCostAndUsage(ctx context.Context, params *costexplorer.GetCostAndUsageInput, _ ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
	args := m.Called(ctx, params)
	output := args.Get(0)
	err := args.Error(1)
	if output == nil {
		return nil, err
	}
	ret, ok := output.(*costexplorer.GetCostAndUsageOutput)
	if !ok {
		m.t.Fatalf("unexpected type: %T", output)
	}
	return ret, err
}

func TestGraphGenerator(t *testing.T) {
	bs, err := os.ReadFile("testdata/anomaly.json")
	require.NoError(t, err)
	var a Anomaly
	err = json.Unmarshal(bs, &a)
	require.NoError(t, err)

	mockClient := mockGetCostAndUsageAPIClient{t: t}
	defer mockClient.AssertExpectations(t)
	mockClient.On("GetCostAndUsage", mock.Anything, &costexplorer.GetCostAndUsageInput{
		Granularity: types.GranularityDaily,
		Metrics:     []string{"NET_UNBLENDED_COST"},
		TimePeriod: &types.DateInterval{
			Start: aws.String("2021-05-17"),
			End:   aws.String("2021-06-02"),
		},
		Filter: &types.Expression{
			And: []types.Expression{
				{
					Dimensions: &types.DimensionValues{
						Key:    types.DimensionRecordType,
						Values: []string{"Usage"},
					},
				},
				{
					Dimensions: &types.DimensionValues{
						Key:    types.DimensionLinkedAccount,
						Values: []string{"123456789012"},
					},
				},
				{
					Dimensions: &types.DimensionValues{
						Key:    types.DimensionRegion,
						Values: []string{"ap-northeast-1"},
					},
				},
				{
					Dimensions: &types.DimensionValues{
						Key:    types.DimensionService,
						Values: []string{"Amazon Elastic Compute Cloud - Compute"},
					},
				},
				{
					Dimensions: &types.DimensionValues{
						Key:    types.DimensionUsageType,
						Values: []string{"AnomalousUsageType"},
					},
				},
			},
		},
		GroupBy: []types.GroupDefinition{},
	}).Return(&costexplorer.GetCostAndUsageOutput{
		ResultsByTime: []types.ResultByTime{
			{
				TimePeriod: &types.DateInterval{
					Start: aws.String("2021-05-17"),
					End:   aws.String("2021-05-17"),
				},
				Total: map[string]types.MetricValue{
					"NetUnblendedCost": {
						Amount: aws.String("1.25"),
						Unit:   aws.String("USD"),
					},
				},
			},
			{
				TimePeriod: &types.DateInterval{
					Start: aws.String("2021-05-18"),
					End:   aws.String("2021-05-18"),
				},
				Total: map[string]types.MetricValue{
					"NetUnblendedCost": {
						Amount: aws.String("1.25"),
						Unit:   aws.String("USD"),
					},
				},
			},
			{
				TimePeriod: &types.DateInterval{
					Start: aws.String("2021-05-19"),
					End:   aws.String("2021-05-19"),
				},
				Total: map[string]types.MetricValue{
					"NetUnblendedCost": {
						Amount: aws.String("1.25"),
						Unit:   aws.String("USD"),
					},
				},
			},
			{
				TimePeriod: &types.DateInterval{
					Start: aws.String("2021-05-20"),
					End:   aws.String("2021-05-20"),
				},
				Total: map[string]types.MetricValue{
					"NetUnblendedCost": {
						Amount: aws.String("1.75"),
						Unit:   aws.String("USD"),
					},
				},
			},
		},
	}, nil)

	gen := NewGraphGenerator(&mockClient)
	ctx := context.Background()
	graphs, err := gen.Generate(ctx, a)
	require.NoError(t, err)

	g := goldie.New(
		t,
		goldie.WithFixtureDir("testdata/graphs"),
		goldie.WithNameSuffix(".golden.png"),
	)
	for i, graph := range graphs {
		bs, err := io.ReadAll(graph.r)
		require.NoError(t, err)
		g.Assert(t, fmt.Sprintf("graph%d", i), bs)
		if graph.size != int64(len(bs)) {
			t.Errorf("unexpected size graph%d: want=%d got=%d", i, graph.size, len(bs))
		}
	}
}
