// Package costexplorerx provides paginators for AWS Cost Explorer API calls
// that are not covered by the official aws-sdk-go-v2 paginator helpers.
package costexplorerx

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
)

// GetAnomaliesAPIClient is the subset of the Cost Explorer client used by
// GetAnomaliesPaginator.
type GetAnomaliesAPIClient interface {
	GetAnomalies(context.Context, *costexplorer.GetAnomaliesInput, ...func(*costexplorer.Options)) (*costexplorer.GetAnomaliesOutput, error)
}

var _ GetAnomaliesAPIClient = (*costexplorer.Client)(nil)

// GetAnomaliesPaginator paginates over Cost Explorer GetAnomalies results.
type GetAnomaliesPaginator struct {
	client    GetAnomaliesAPIClient
	params    *costexplorer.GetAnomaliesInput
	nextToken *string
	firstPage bool
}

// NewGetAnomaliesPaginator returns a new paginator for GetAnomalies.
func NewGetAnomaliesPaginator(client GetAnomaliesAPIClient, params *costexplorer.GetAnomaliesInput) *GetAnomaliesPaginator {
	return &GetAnomaliesPaginator{
		client:    client,
		params:    params,
		firstPage: true,
	}
}

// HasMorePages reports whether there are more pages to fetch.
func (p *GetAnomaliesPaginator) HasMorePages() bool {
	return p.firstPage || (p.nextToken != nil && len(*p.nextToken) != 0)
}

// NextPage fetches the next page of GetAnomalies results.
func (p *GetAnomaliesPaginator) NextPage(ctx context.Context, optFns ...func(*costexplorer.Options)) (*costexplorer.GetAnomaliesOutput, error) {
	if !p.HasMorePages() {
		return nil, nil
	}

	params := *p.params
	params.NextPageToken = p.nextToken

	result, err := p.client.GetAnomalies(ctx, &params, optFns...)
	if err != nil {
		return nil, err
	}
	p.firstPage = false
	p.nextToken = result.NextPageToken

	return result, nil
}
