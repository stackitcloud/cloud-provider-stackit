package client

import (
	"context"

	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
)

type iaasClient struct {
	Client    iaas.DefaultAPI
	projectID string
	region    string
}

type IaaSClient interface {
	GetServer(ctx context.Context, serverID string) (*iaas.Server, error)
	ListServers(ctx context.Context) (*[]iaas.Server, error)
}

func NewIaaSClient(region, projectID string, options []sdkconfig.ConfigurationOption) (IaaSClient, error) {
	apiClient, err := iaas.NewAPIClient(options...)
	if err != nil {
		return nil, err
	}
	return &iaasClient{
		Client:    apiClient.DefaultAPI,
		projectID: projectID,
		region:    region,
	}, nil
}

func (i *iaasClient) GetServer(ctx context.Context, serverID string) (*iaas.Server, error) {
	return withResponseID(ctx, func(ctx context.Context) (*iaas.Server, error) {
		return i.Client.GetServer(ctx, i.projectID, i.region, serverID).Details(true).Execute()
	})
}

func (i *iaasClient) ListServers(ctx context.Context) (*[]iaas.Server, error) {
	return withResponseID(ctx, func(ctx context.Context) (*[]iaas.Server, error) {
		resp, err := i.Client.ListServers(ctx, i.projectID, i.region).Details(true).Execute()
		if err != nil {
			return nil, err
		}

		return &resp.Items, nil
	})
}
