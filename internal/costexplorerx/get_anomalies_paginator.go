package costexplorerx

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
)

type GetAnomaliesAPIClient interface {
	GetAnomalies(context.Context, *costexplorer.GetAnomaliesInput, ...func(*costexplorer.Options)) (*costexplorer.GetAnomaliesOutput, error)
}

var _ GetAnomaliesAPIClient = (*costexplorer.Client)(nil)

type GetAnomaliesPaginator struct {
	client    GetAnomaliesAPIClient
	params    *costexplorer.GetAnomaliesInput
	nextToken *string
	firstPage bool
}

func NewGetAnomaliesPaginator(client GetAnomaliesAPIClient, params *costexplorer.GetAnomaliesInput) *GetAnomaliesPaginator {
	return &GetAnomaliesPaginator{
		client:    client,
		params:    params,
		firstPage: true,
	}
}

func (p *GetAnomaliesPaginator) HasMorePages() bool {
	return p.firstPage || (p.nextToken != nil && len(*p.nextToken) != 0)
}

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
