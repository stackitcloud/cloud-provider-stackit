package client

import (
	"context"
	"net/http"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/stackiterrors"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	"github.com/stackitcloud/stackit-sdk-go/core/runtime"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	sdkWait "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api/wait"
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
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	server, err := i.Client.GetServer(ctx, i.projectID, i.region, serverID).Details(true).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return server, nil
}

func (i *iaasClient) ListServers(ctx context.Context) (*[]iaas.Server, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	resp, err := i.Client.ListServers(ctx, i.projectID, i.region).Details(true).Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return &resp.Items, nil
}
