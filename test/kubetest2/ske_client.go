package kubetest2

import (
	"context"
	"fmt"
	"strconv"

	"github.com/stackitcloud/stackit-sdk-go/services/ske"
	skewait "github.com/stackitcloud/stackit-sdk-go/services/ske/wait"
	"k8s.io/klog/v2"
)

type skeClient interface {
	GetCluster(ctx context.Context, projectID, region, name string) (*ske.Cluster, error)
	ListProviderOptions(ctx context.Context, region string) (*ske.ProviderOptions, error)
	CreateOrUpdateCluster(ctx context.Context, projectID, region, name string, payload ske.CreateOrUpdateClusterPayload) (*ske.Cluster, error)
	WaitForClusterReady(ctx context.Context, projectID, region, name string) (*ske.Cluster, error)
	CreateKubeconfig(ctx context.Context, projectID, region, name string, expirationSeconds int64) (*ske.Kubeconfig, error)
	DeleteCluster(ctx context.Context, projectID, region, name string) error
	WaitForClusterDeleted(ctx context.Context, projectID, region, name string) error
}

type sdkSKEClient struct {
	api ske.DefaultApi
}

func newSKEClient(region, serviceAccount, endpoint string) (skeClient, error) {
	klog.Infof("Creating SKE API client for region=%q with service_account_bytes=%d", region, len(serviceAccount))

	apiClient, err := ske.NewAPIClient(apiClientOptions(serviceAccount, endpoint, "ske")...)
	if err != nil {
		return nil, fmt.Errorf("create SKE client: %w", err)
	}

	klog.Infof("Created SKE API client successfully")

	return &sdkSKEClient{api: apiClient}, nil
}

func (c *sdkSKEClient) GetCluster(ctx context.Context, projectID, region, name string) (*ske.Cluster, error) {
	klog.Infof("SKE GetCluster: project_id=%q region=%q cluster=%q", projectID, region, name)
	return c.api.GetCluster(ctx, projectID, region, name).Execute()
}

func (c *sdkSKEClient) ListProviderOptions(ctx context.Context, region string) (*ske.ProviderOptions, error) {
	klog.Infof("SKE ListProviderOptions: region=%q", region)
	return c.api.ListProviderOptions(ctx, region).Execute()
}

func (c *sdkSKEClient) CreateOrUpdateCluster(ctx context.Context, projectID, region, name string, payload ske.CreateOrUpdateClusterPayload) (*ske.Cluster, error) {
	klog.Infof("SKE CreateOrUpdateCluster: project_id=%q region=%q cluster=%q", projectID, region, name)
	return c.api.CreateOrUpdateCluster(ctx, projectID, region, name).CreateOrUpdateClusterPayload(payload).Execute()
}

func (c *sdkSKEClient) WaitForClusterReady(ctx context.Context, projectID, region, name string) (*ske.Cluster, error) {
	klog.Infof("Waiting for SKE cluster to become ready: project_id=%q region=%q cluster=%q", projectID, region, name)
	cluster, err := skewait.CreateOrUpdateClusterWaitHandler(ctx, c.api, projectID, region, name).WaitWithContext(ctx)
	if err != nil {
		return nil, err
	}
	if cluster != nil && cluster.Status != nil && cluster.Status.Aggregated != nil {
		klog.Infof("SKE cluster is ready: cluster=%q state=%q", name, cluster.Status.GetAggregated())
	}
	return cluster, nil
}

func (c *sdkSKEClient) CreateKubeconfig(ctx context.Context, projectID, region, name string, expirationSeconds int64) (*ske.Kubeconfig, error) {
	klog.Infof(
		"SKE CreateKubeconfig: project_id=%q region=%q cluster=%q expiration_seconds=%d",
		projectID,
		region,
		name,
		expirationSeconds,
	)
	payload := ske.NewCreateKubeconfigPayload()
	payload.SetExpirationSeconds(strconv.FormatInt(expirationSeconds, 10))
	return c.api.CreateKubeconfig(ctx, projectID, region, name).CreateKubeconfigPayload(*payload).Execute()
}

func (c *sdkSKEClient) DeleteCluster(ctx context.Context, projectID, region, name string) error {
	klog.Infof("SKE DeleteCluster: project_id=%q region=%q cluster=%q", projectID, region, name)
	_, err := c.api.DeleteCluster(ctx, projectID, region, name).Execute()
	return err
}

func (c *sdkSKEClient) WaitForClusterDeleted(ctx context.Context, projectID, region, name string) error {
	klog.Infof("Waiting for SKE cluster deletion: project_id=%q region=%q cluster=%q", projectID, region, name)
	_, err := skewait.DeleteClusterWaitHandler(ctx, c.api, projectID, region, name).WaitWithContext(ctx)
	return err
}
