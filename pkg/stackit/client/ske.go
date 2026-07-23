package client

import (
	"context"

	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	ske "github.com/stackitcloud/stackit-sdk-go/services/ske/v2api"
	skewait "github.com/stackitcloud/stackit-sdk-go/services/ske/v2api/wait"
)

type skeClient struct {
	Client    ske.DefaultAPI
	projectID string
	region    string
}

type SKEClient interface {
	ListProviderOptions(ctx context.Context) (*ske.ProviderOptions, error)
	CreateOrUpdateCluster(ctx context.Context, clusterName string, payload ske.CreateOrUpdateClusterPayload) (*ske.Cluster, error)
	GetCluster(ctx context.Context, clusterName string) (*ske.Cluster, error)
	DeleteCluster(ctx context.Context, clusterName string) error
	CreateKubeconfig(ctx context.Context, clusterName string, payload ske.CreateKubeconfigPayload) (*ske.Kubeconfig, error)
	WaitClusterReady(ctx context.Context, clusterName string) (*ske.Cluster, error)
	WaitClusterDeleted(ctx context.Context, clusterName string) error
}

func NewSKEClient(region, projectID string, options []sdkconfig.ConfigurationOption) (SKEClient, error) {
	apiClient, err := ske.NewAPIClient(options...)
	if err != nil {
		return nil, err
	}

	return &skeClient{
		Client:    apiClient.DefaultAPI,
		projectID: projectID,
		region:    region,
	}, nil
}

func (s *skeClient) ListProviderOptions(ctx context.Context) (*ske.ProviderOptions, error) {
	return withResponseID(ctx, func(ctx context.Context) (*ske.ProviderOptions, error) {
		return s.Client.ListProviderOptions(ctx, s.region).Execute()
	})
}

//nolint:gocritic // Payload is passed by value to match the shared SKEClient interface.
func (s *skeClient) CreateOrUpdateCluster(ctx context.Context, clusterName string, payload ske.CreateOrUpdateClusterPayload) (*ske.Cluster, error) {
	return withResponseID(ctx, func(ctx context.Context) (*ske.Cluster, error) {
		return s.Client.
			CreateOrUpdateCluster(ctx, s.projectID, s.region, clusterName).
			CreateOrUpdateClusterPayload(payload).
			Execute()
	})
}

func (s *skeClient) GetCluster(ctx context.Context, clusterName string) (*ske.Cluster, error) {
	return withResponseID(ctx, func(ctx context.Context) (*ske.Cluster, error) {
		return s.Client.GetCluster(ctx, s.projectID, s.region, clusterName).Execute()
	})
}

func (s *skeClient) DeleteCluster(ctx context.Context, clusterName string) error {
	_, err := withResponseID(ctx, func(ctx context.Context) (map[string]interface{}, error) {
		return s.Client.DeleteCluster(ctx, s.projectID, s.region, clusterName).Execute()
	})
	return err
}

//nolint:gocritic // Payload is passed by value to match the shared SKEClient interface.
func (s *skeClient) CreateKubeconfig(ctx context.Context, clusterName string, payload ske.CreateKubeconfigPayload) (*ske.Kubeconfig, error) {
	return withResponseID(ctx, func(ctx context.Context) (*ske.Kubeconfig, error) {
		return s.Client.
			CreateKubeconfig(ctx, s.projectID, s.region, clusterName).
			CreateKubeconfigPayload(payload).
			Execute()
	})
}

func (s *skeClient) WaitClusterReady(ctx context.Context, clusterName string) (*ske.Cluster, error) {
	return skewait.CreateOrUpdateClusterWaitHandler(ctx, s.Client, s.projectID, s.region, clusterName).WaitWithContext(ctx)
}

func (s *skeClient) WaitClusterDeleted(ctx context.Context, clusterName string) error {
	_, err := skewait.DeleteClusterWaitHandler(ctx, s.Client, s.projectID, s.region, clusterName).WaitWithContext(ctx)
	return err
}
