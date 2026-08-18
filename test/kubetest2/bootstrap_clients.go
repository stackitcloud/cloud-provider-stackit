package kubetest2

import (
	"context"
	"fmt"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/metrics"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	authorization "github.com/stackitcloud/stackit-sdk-go/services/authorization/v2api"
	resourcemanager "github.com/stackitcloud/stackit-sdk-go/services/resourcemanager/v0api"
	resourcemanagerwait "github.com/stackitcloud/stackit-sdk-go/services/resourcemanager/v0api/wait"
	serviceaccount "github.com/stackitcloud/stackit-sdk-go/services/serviceaccount/v2api"
)

const (
	projectLabelScopeKey             = "scope"
	projectLabelScopeValue           = "PUBLIC"
	projectLabelManagedKey           = "kt2_managed"
	projectLabelManagedValue         = "true"
	projectLabelRunIDKey             = "kt2_run_id"
	projectOwnerRole                 = "owner"
	projectResourceType              = "project"
	childProjectRole                 = "ske.admin"
	projectListPageSize      float32 = 100
)

type projectClient interface {
	ListProjects(ctx context.Context, parentContainerID string) ([]resourcemanager.Project, error)
	CreateProject(ctx context.Context, parentContainerID, name, ownerEmail string, labels map[string]string) (*resourcemanager.Project, error)
	WaitForProjectActive(ctx context.Context, containerID string) (*resourcemanager.GetProjectResponse, error)
	DeleteProject(ctx context.Context, projectID string) error
	WaitForProjectDeleted(ctx context.Context, projectID string) error
}

type serviceAccountClient interface {
	ListServiceAccounts(ctx context.Context, projectID string) ([]serviceaccount.ServiceAccount, error)
	CreateServiceAccount(ctx context.Context, projectID, name string) (*serviceaccount.ServiceAccount, error)
	CreateServiceAccountKey(ctx context.Context, projectID, serviceAccountEmail string) (*serviceaccount.CreateServiceAccountKeyResponse, error)
}

type authorizationClient interface {
	ListMembers(ctx context.Context, resourceType, resourceID string) ([]authorization.Member, error)
	AddMembers(ctx context.Context, resourceID, resourceType string, members []authorization.Member) error
}

type sdkProjectClient struct {
	api *resourcemanager.APIClient
}

type sdkServiceAccountClient struct {
	api *serviceaccount.APIClient
}

type sdkAuthorizationClient struct {
	api *authorization.APIClient
}

func apiClientOptions(serviceAccountKey, endpoint, apiName string) []sdkconfig.ConfigurationOption {
	opts := []sdkconfig.ConfigurationOption{
		sdkconfig.WithServiceAccountKey(serviceAccountKey),
		sdkconfig.WithHTTPClient(metrics.NewInstrumentedHTTPClient(apiName)),
	}
	if endpoint != "" {
		opts = append(opts, sdkconfig.WithEndpoint(endpoint))
	}
	return opts
}

func apiEndpointURL(cfg *sdkconfig.Configuration) string {
	if cfg == nil || len(cfg.Servers) == 0 {
		return "unknown"
	}
	return cfg.Servers[0].URL
}

func newProjectClient(serviceAccountKey, endpoint string) (projectClient, error) {
	apiClient, err := resourcemanager.NewAPIClient(apiClientOptions(serviceAccountKey, endpoint, "resourcemanager")...)
	if err != nil {
		return nil, fmt.Errorf("create Resource Manager client: %w", err)
	}
	return &sdkProjectClient{api: apiClient}, nil
}

func newServiceAccountClient(serviceAccountKey, endpoint string) (serviceAccountClient, error) {
	apiClient, err := serviceaccount.NewAPIClient(apiClientOptions(serviceAccountKey, endpoint, "serviceaccount")...)
	if err != nil {
		return nil, fmt.Errorf("create Service Account client: %w", err)
	}
	return &sdkServiceAccountClient{api: apiClient}, nil
}

func newAuthorizationClient(serviceAccountKey, endpoint string) (authorizationClient, error) {
	apiClient, err := authorization.NewAPIClient(apiClientOptions(serviceAccountKey, endpoint, "authorization")...)
	if err != nil {
		return nil, fmt.Errorf("create Authorization client: %w", err)
	}
	return &sdkAuthorizationClient{api: apiClient}, nil
}

func (c *sdkProjectClient) ListProjects(ctx context.Context, parentContainerID string) ([]resourcemanager.Project, error) {
	projects := make([]resourcemanager.Project, 0)
	var offset float32
	for {
		resp, err := c.api.DefaultAPI.ListProjects(ctx).
			ContainerParentId(parentContainerID).
			Offset(offset).
			Limit(projectListPageSize).
			Execute()
		if err != nil {
			return nil, err
		}
		items := resp.GetItems()
		projects = append(projects, items...)
		if len(items) < int(projectListPageSize) {
			return projects, nil
		}
		offset += float32(len(items))
	}
}

func (c *sdkProjectClient) CreateProject(ctx context.Context, parentContainerID, name, ownerEmail string, labels map[string]string) (*resourcemanager.Project, error) {
	payload := resourcemanager.NewCreateProjectPayload(
		parentContainerID,
		[]resourcemanager.Member{*resourcemanager.NewMember(projectOwnerRole, ownerEmail)},
		name,
	)
	payload.SetLabels(labels)
	return c.api.DefaultAPI.CreateProject(ctx).CreateProjectPayload(*payload).Execute()
}

func (c *sdkProjectClient) WaitForProjectActive(ctx context.Context, containerID string) (*resourcemanager.GetProjectResponse, error) {
	return resourcemanagerwait.CreateProjectWaitHandler(ctx, c.api.DefaultAPI, containerID).WaitWithContext(ctx)
}

func (c *sdkProjectClient) DeleteProject(ctx context.Context, projectID string) error {
	return c.api.DefaultAPI.DeleteProject(ctx, projectID).Execute()
}

func (c *sdkProjectClient) WaitForProjectDeleted(ctx context.Context, projectID string) error {
	_, err := resourcemanagerwait.DeleteProjectWaitHandler(ctx, c.api.DefaultAPI, projectID).WaitWithContext(ctx)
	return err
}

func (c *sdkServiceAccountClient) ListServiceAccounts(ctx context.Context, projectID string) ([]serviceaccount.ServiceAccount, error) {
	resp, err := c.api.DefaultAPI.ListServiceAccounts(ctx, projectID).Execute()
	if err != nil {
		return nil, err
	}
	return resp.GetItems(), nil
}

func (c *sdkServiceAccountClient) CreateServiceAccount(ctx context.Context, projectID, name string) (*serviceaccount.ServiceAccount, error) {
	payload := serviceaccount.NewCreateServiceAccountPayload(name)
	return c.api.DefaultAPI.CreateServiceAccount(ctx, projectID).CreateServiceAccountPayload(*payload).Execute()
}

func (c *sdkServiceAccountClient) CreateServiceAccountKey(ctx context.Context, projectID, serviceAccountEmail string) (*serviceaccount.CreateServiceAccountKeyResponse, error) {
	payload := serviceaccount.NewCreateServiceAccountKeyPayloadWithDefaults()
	resp, err := c.api.DefaultAPI.CreateServiceAccountKey(ctx, projectID, serviceAccountEmail).CreateServiceAccountKeyPayload(*payload).Execute()
	if err != nil {
		return nil, fmt.Errorf("%w (endpoint: %s)", err, apiEndpointURL(c.api.GetConfig()))
	}
	return resp, nil
}

func (c *sdkAuthorizationClient) ListMembers(ctx context.Context, resourceType, resourceID string) ([]authorization.Member, error) {
	resp, err := c.api.DefaultAPI.ListMembers(ctx, resourceType, resourceID).Execute()
	if err != nil {
		return nil, err
	}
	return resp.GetMembers(), nil
}

func (c *sdkAuthorizationClient) AddMembers(ctx context.Context, resourceID, resourceType string, members []authorization.Member) error {
	payload := authorization.NewAddMembersPayload(members, resourceType)
	_, err := c.api.DefaultAPI.AddMembers(ctx, resourceID).AddMembersPayload(*payload).Execute()
	return err
}
