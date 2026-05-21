package costexplorerx

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
)

// GetCostAndUsageAPIClient is the subset of the Cost Explorer client used by
// GetCostAndUsagePaginator.
type GetCostAndUsageAPIClient interface {
	GetCostAndUsage(context.Context, *costexplorer.GetCostAndUsageInput, ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error)
}

var _ GetCostAndUsageAPIClient = (*costexplorer.Client)(nil)

// GetCostAndUsagePaginator paginates over Cost Explorer GetCostAndUsage results.
type GetCostAndUsagePaginator struct {
	client    GetCostAndUsageAPIClient
	params    *costexplorer.GetCostAndUsageInput
	nextToken *string
	firstPage bool
}

// NewGetCostAndUsagePaginator returns a new paginator for GetCostAndUsage.
func NewGetCostAndUsagePaginator(client GetCostAndUsageAPIClient, params *costexplorer.GetCostAndUsageInput) *GetCostAndUsagePaginator {
	return &GetCostAndUsagePaginator{
		client:    client,
		params:    params,
		firstPage: true,
	}
}

// HasMorePages reports whether there are more pages to fetch.
func (p *GetCostAndUsagePaginator) HasMorePages() bool {
	return p.firstPage || (p.nextToken != nil && len(*p.nextToken) != 0)
}

// NextPage fetches the next page of GetCostAndUsage results.
func (p *GetCostAndUsagePaginator) NextPage(ctx context.Context, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
	if !p.HasMorePages() {
		return nil, nil
	}

	params := *p.params
	params.NextPageToken = p.nextToken

	result, err := p.client.GetCostAndUsage(ctx, &params, optFns...)
	if err != nil {
		return nil, err
	}
	p.firstPage = false
	p.nextToken = result.NextPageToken

	return result, nil
}
