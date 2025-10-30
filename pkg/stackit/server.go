package stackit

import (
	"context"

	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
)

func (cl nodeClient) GetServer(ctx context.Context, projectID, region, serverID string) (*iaas.Server, error) {
	server, err := cl.client.GetServer(ctx, projectID, region, serverID).Details(true).Execute()
	if isOpenAPINotFound(err) {
		return server, ErrorNotFound
	}
	return server, err
}

func (cl nodeClient) DeleteServer(ctx context.Context, projectID, region, serverID string) error {
	return cl.client.DeleteServerExecute(ctx, projectID, region, serverID)
}

func (cl nodeClient) CreateServer(ctx context.Context, projectID, region string, create *iaas.CreateServerPayload) (*iaas.Server, error) {
	server, err := cl.client.CreateServer(ctx, projectID, region).CreateServerPayload(*create).Execute()
	if isOpenAPINotFound(err) {
		return server, ErrorNotFound
	}
	return server, err
}

func (cl nodeClient) UpdateServer(ctx context.Context, projectID, region, serverID string, update *iaas.UpdateServerPayload) (*iaas.Server, error) {
	return cl.client.UpdateServer(ctx, projectID, region, serverID).UpdateServerPayload(*update).Execute()
}

func (cl nodeClient) ListServers(ctx context.Context, projectID, region string) (*[]iaas.Server, error) {
	resp, err := cl.client.ListServersExecute(ctx, projectID, region)
	if err != nil {
		return nil, err
	}
	return resp.Items, nil
}
