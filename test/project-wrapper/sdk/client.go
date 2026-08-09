package sdk

import (
	"context"

	resourcemanager "github.com/stackitcloud/stackit-sdk-go/services/resourcemanager/v0api"
)

type Client struct {
	rmClient *resourcemanager.APIClient
}

func NewClient() (*Client, error) {
	rmClient, err := resourcemanager.NewAPIClient()
	if err != nil {
		return nil, err
	}

	return &Client{
		rmClient: rmClient,
	}, nil
}

func (c *Client) CreateProject(
	ctx context.Context,
	organizationID string,
	name string,
	labels map[string]string,
	subject string,
) (*resourcemanager.Project, error) {
	payload := resourcemanager.CreateProjectPayload{
		Labels: new(labels),
		Members: []resourcemanager.Member{
			{
				Role:    "owner",
				Subject: subject,
			},
		},
		Name:              name,
		ContainerParentId: organizationID,
	}
	project, err := c.rmClient.DefaultAPI.CreateProject(ctx).CreateProjectPayload(payload).Execute()
	if err != nil {
		return nil, err
	}
	return project, nil
}

func (c *Client) GetProject(ctx context.Context, projectID string) (*resourcemanager.GetProjectResponse, error) {
	project, err := c.rmClient.DefaultAPI.GetProject(ctx, projectID).Execute()
	if err != nil {
		return nil, err
	}
	return project, nil
}

func (c *Client) DeleteProject(ctx context.Context, projectID string) error {
	return c.rmClient.DefaultAPI.DeleteProject(ctx, projectID).Execute()
}
