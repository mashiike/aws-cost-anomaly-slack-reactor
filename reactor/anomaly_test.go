package reactor

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	organizationstypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "update .golden.png files")

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

type mockDescribeAccountAPIClient struct {
	mock.Mock
	t *testing.T
}

func (m *mockDescribeAccountAPIClient) DescribeAccount(ctx context.Context, params *organizations.DescribeAccountInput, _ ...func(*organizations.Options)) (*organizations.DescribeAccountOutput, error) {
	args := m.Called(ctx, params)
	output := args.Get(0)
	err := args.Error(1)
	if output == nil {
		return nil, err
	}
	ret, ok := output.(*organizations.DescribeAccountOutput)
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
	mockOrgClient := mockDescribeAccountAPIClient{t: t}
	defer mockClient.AssertExpectations(t)
	defer mockOrgClient.AssertExpectations(t)
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

	gen := NewGraphGenerator(&mockClient, &mockOrgClient)
	ctx := context.Background()
	graphs, err := gen.Generate(ctx, a)
	require.NoError(t, err)

	for i, graph := range graphs {
		bs, err := io.ReadAll(graph.r)
		require.NoError(t, err)
		if graph.size != int64(len(bs)) {
			t.Errorf("unexpected size graph%d: want=%d got=%d", i, graph.size, len(bs))
		}
		if *update {
			os.MkdirAll("testdata/fixture/", 0755)
			err := os.WriteFile(fmt.Sprintf("testdata/fixture/test_graph_generator_%d.golden.png", i), bs, 0644)
			if err != nil {
				t.Fatalf("failed to update golden file: %v", err)
			}
		}
		expected, err := os.ReadFile(fmt.Sprintf("testdata/fixture/test_graph_generator_%d.golden.png", i))
		require.NoError(t, err)
		if !reflect.DeepEqual(expected, bs) {
			t.Errorf("unexpected graph%d", i)
		}
	}
}

func TestGraphGeneratorForOrganization(t *testing.T) {
	bs, err := os.ReadFile("testdata/anomaly_org.json")
	require.NoError(t, err)
	var a Anomaly
	err = json.Unmarshal(bs, &a)
	require.NoError(t, err)

	mockClient := mockGetCostAndUsageAPIClient{t: t}
	mockOrgClient := mockDescribeAccountAPIClient{t: t}
	defer mockClient.AssertExpectations(t)
	defer mockOrgClient.AssertExpectations(t)

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
						Key:    types.DimensionService,
						Values: []string{"Amazon Simple Notification Service"},
					},
				},
			},
		},
		GroupBy: []types.GroupDefinition{},
	}).Return(&costexplorer.GetCostAndUsageOutput{
		GroupDefinitions: []types.GroupDefinition{
			{
				Key:  aws.String("LINKED_ACCOUNT"),
				Type: types.GroupDefinitionTypeDimension,
			},
		},
		ResultsByTime: []types.ResultByTime{
			{
				TimePeriod: &types.DateInterval{
					Start: aws.String("2021-05-17"),
					End:   aws.String("2021-05-17"),
				},
				Groups: []types.Group{
					{
						Keys: []string{"123456789012"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("1.25"),
								Unit:   aws.String("USD"),
							},
						},
					},
					{
						Keys: []string{"234567890123"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("2.24"),
								Unit:   aws.String("USD"),
							},
						},
					},
					{
						Keys: []string{"345678901234"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("3.33"),
								Unit:   aws.String("USD"),
							},
						},
					},
				},
				Total: map[string]types.MetricValue{},
			},
			{
				TimePeriod: &types.DateInterval{
					Start: aws.String("2021-05-18"),
					End:   aws.String("2021-05-18"),
				},
				Groups: []types.Group{
					{
						Keys: []string{"123456789012"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("1.25"),
								Unit:   aws.String("USD"),
							},
						},
					},
					{
						Keys: []string{"456789012345"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("4.56"),
								Unit:   aws.String("USD"),
							},
						},
					},
					{
						Keys: []string{"567890123456"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("0.22"),
								Unit:   aws.String("USD"),
							},
						},
					},
					{
						Keys: []string{"678901234567"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("1.78"),
								Unit:   aws.String("USD"),
							},
						},
					},
				},
				Total: map[string]types.MetricValue{},
			},
			{
				TimePeriod: &types.DateInterval{
					Start: aws.String("2021-05-19"),
					End:   aws.String("2021-05-19"),
				},
				Groups: []types.Group{
					{
						Keys: []string{"123456789012"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("4.25"),
								Unit:   aws.String("USD"),
							},
						},
					},
					{
						Keys: []string{"234567890123"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("2.24"),
								Unit:   aws.String("USD"),
							},
						},
					},
					{
						Keys: []string{"890123456789"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("4.89"),
								Unit:   aws.String("USD"),
							},
						},
					},
				},
				Total: map[string]types.MetricValue{},
			},
			{
				TimePeriod: &types.DateInterval{
					Start: aws.String("2021-05-20"),
					End:   aws.String("2021-05-20"),
				},
				Groups: []types.Group{
					{
						Keys: []string{"123456789012"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("1.75"),
								Unit:   aws.String("USD"),
							},
						},
					},
					{
						Keys: []string{"234567890123"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("2.24"),
								Unit:   aws.String("USD"),
							},
						},
					},
					{
						Keys: []string{"321098765432"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("2.24"),
								Unit:   aws.String("USD"),
							},
						},
					},
					{
						Keys: []string{"432109876543"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("0.24"),
								Unit:   aws.String("USD"),
							},
						},
					},
				},
				Total: map[string]types.MetricValue{},
			},
			{
				TimePeriod: &types.DateInterval{
					Start: aws.String("2021-05-21"),
					End:   aws.String("2021-05-21"),
				},
				Groups: []types.Group{
					{
						Keys: []string{"123456789012"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("1.25"),
								Unit:   aws.String("USD"),
							},
						},
					},
					{
						Keys: []string{"543210987654"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("5.43"),
								Unit:   aws.String("USD"),
							},
						},
					},
					{
						Keys: []string{"643210987652"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("6.43"),
								Unit:   aws.String("USD"),
							},
						},
					},
					{
						Keys: []string{"743210987651"},
						Metrics: map[string]types.MetricValue{
							"NetUnblendedCost": {
								Amount: aws.String("7.43"),
								Unit:   aws.String("USD"),
							},
						},
					},
				},
				Total: map[string]types.MetricValue{},
			},
		},
	}, nil)

	awsAccounts := map[string]string{
		"123456789012": "aws-account1",
		"234567890123": "aws-account2",
		"345678901234": "aws-account3",
		"456789012345": "aws-account4",
		"567890123456": "aws-account5",
		"321098765432": "aws-account8",
		"432109876543": "aws-account9",
		"543210987654": "aws-account10",
		"643210987652": "aws-account11",
		"743210987651": "aws-account12",
	}
	for accountID, accountName := range awsAccounts {
		mockOrgClient.On("DescribeAccount", mock.Anything, &organizations.DescribeAccountInput{
			AccountId: aws.String(accountID),
		}).Return(&organizations.DescribeAccountOutput{
			Account: &organizationstypes.Account{
				Arn:   aws.String(fmt.Sprintf("arn:aws:organizations::123456789012:account/%s", accountName)),
				Email: aws.String(fmt.Sprintf("%s@example.com", accountName)),
				Id:    aws.String(accountID),
				Name:  aws.String(accountName),
			},
		}, nil).Times(1)
	}
	accessDenidedAWSAccounts := []string{
		"678901234567",
		"890123456789",
	}
	for _, accountID := range accessDenidedAWSAccounts {
		mockOrgClient.On("DescribeAccount", mock.Anything, &organizations.DescribeAccountInput{
			AccountId: aws.String(accountID),
		}).Return(nil, &smithy.GenericAPIError{Code: "AccessDeniedException"}).Times(1)
	}
	gen := NewGraphGenerator(&mockClient, &mockOrgClient)
	ctx := context.Background()
	graphs, err := gen.Generate(ctx, a)
	require.NoError(t, err)

	for i, graph := range graphs {
		bs, err := io.ReadAll(graph.r)
		require.NoError(t, err)
		if graph.size != int64(len(bs)) {
			t.Errorf("unexpected size graph%d: want=%d got=%d", i, graph.size, len(bs))
		}
		require.True(t, graph.size > 0)
		fileName := fmt.Sprintf("testdata/fixture/test_graph_generator_for_org_%d.golden.png", i)
		if *update {
			os.MkdirAll("testdata/fixture/", 0755)
			err := os.WriteFile(fileName, bs, 0644)
			if err != nil {
				t.Fatalf("failed to update golden file: %v", err)
			}
			t.Logf("updated golden file: %s", fileName)
		}
		expected, err := os.ReadFile(fileName)
		require.NoError(t, err)
		if !reflect.DeepEqual(expected, bs) {
			t.Errorf("unexpected graph_%d", i)
		}
	}
}
