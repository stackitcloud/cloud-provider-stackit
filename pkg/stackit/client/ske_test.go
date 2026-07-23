package client

import (
	"context"
	"net/http"
	"testing"

	oapierror "github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	ske "github.com/stackitcloud/stackit-sdk-go/services/ske/v2api"
)

func TestSKEClientListProviderOptions(t *testing.T) {
	expectedVersion := "1.32.1"
	api := ske.DefaultAPIServiceMock{
		ListProviderOptionsExecuteMock: testPtr(func(_ ske.ApiListProviderOptionsRequest) (*ske.ProviderOptions, error) {
			return &ske.ProviderOptions{
				KubernetesVersions: []ske.KubernetesVersion{
					{Version: &expectedVersion},
				},
			}, nil
		}),
	}

	c := &skeClient{
		Client: api,
		region: "eu01",
	}

	options, err := c.ListProviderOptions(context.Background())
	if err != nil {
		t.Fatalf("ListProviderOptions() returned error: %v", err)
	}
	if len(options.KubernetesVersions) != 1 || options.KubernetesVersions[0].GetVersion() != expectedVersion {
		t.Fatalf("unexpected provider options: %#v", options)
	}
}

func TestSKEClientCreateOrUpdateClusterPropagatesError(t *testing.T) {
	api := ske.DefaultAPIServiceMock{
		CreateOrUpdateClusterExecuteMock: testPtr(func(_ ske.ApiCreateOrUpdateClusterRequest) (*ske.Cluster, error) {
			return nil, &oapierror.GenericOpenAPIError{StatusCode: http.StatusBadRequest}
		}),
	}

	c := &skeClient{
		Client:    api,
		projectID: "project-id",
		region:    "eu01",
	}

	_, err := c.CreateOrUpdateCluster(context.Background(), "cluster", *ske.NewCreateOrUpdateClusterPayload(
		*ske.NewKubernetes("1.32.1"),
		[]ske.Nodepool{},
	))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSKEClientCreateKubeconfig(t *testing.T) {
	content := "apiVersion: v1\n"
	api := ske.DefaultAPIServiceMock{
		CreateKubeconfigExecuteMock: testPtr(func(_ ske.ApiCreateKubeconfigRequest) (*ske.Kubeconfig, error) {
			return &ske.Kubeconfig{Kubeconfig: &content}, nil
		}),
	}

	c := &skeClient{
		Client:    api,
		projectID: "project-id",
		region:    "eu01",
	}

	kubeconfig, err := c.CreateKubeconfig(context.Background(), "cluster", ske.CreateKubeconfigPayload{})
	if err != nil {
		t.Fatalf("CreateKubeconfig() returned error: %v", err)
	}
	if kubeconfig.GetKubeconfig() != content {
		t.Fatalf("unexpected kubeconfig: %#v", kubeconfig)
	}
}

func TestSKEClientWaitClusterReady(t *testing.T) {
	clusterName := "cluster"
	status := ske.CLUSTERSTATUSSTATE_STATE_HEALTHY
	api := ske.DefaultAPIServiceMock{
		GetClusterExecuteMock: testPtr(func(_ ske.ApiGetClusterRequest) (*ske.Cluster, error) {
			return &ske.Cluster{
				Name: &clusterName,
				Status: &ske.ClusterStatus{
					Aggregated: &status,
				},
				Kubernetes: *ske.NewKubernetes("1.32.1"),
				Nodepools:  []ske.Nodepool{},
			}, nil
		}),
	}

	c := &skeClient{
		Client:    api,
		projectID: "project-id",
		region:    "eu01",
	}

	cluster, err := c.WaitClusterReady(context.Background(), clusterName)
	if err != nil {
		t.Fatalf("WaitClusterReady() returned error: %v", err)
	}
	if cluster.GetName() != clusterName {
		t.Fatalf("unexpected cluster: %#v", cluster)
	}
}

func TestSKEClientWaitClusterDeletedPropagatesError(t *testing.T) {
	api := ske.DefaultAPIServiceMock{
		ListClustersExecuteMock: testPtr(func(_ ske.ApiListClustersRequest) (*ske.ListClustersResponse, error) {
			return nil, &oapierror.GenericOpenAPIError{StatusCode: http.StatusInternalServerError}
		}),
	}

	c := &skeClient{
		Client:    api,
		projectID: "project-id",
		region:    "eu01",
	}

	err := c.WaitClusterDeleted(context.Background(), "cluster")
	if err == nil {
		t.Fatal("expected error")
	}
}

func testPtr[T any](v T) *T {
	return &v
}
