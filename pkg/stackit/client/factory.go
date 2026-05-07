package client

import (
	"context"
	"errors"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/config"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	UserAgent = "cloud-provider-stackit"
)

var (
	ErrorNotFound = errors.New("not found")
)

// Factory produces clients for various STACKIT services.
type Factory interface {
	// LoadBalancing returns a STACKIT load balancing service client.
	LoadBalancing(context.Context) (LoadBalancingClient, error)

	// IaaS returns a STACKIT IaaS service client.
	IaaS(context.Context, client.Client, corev1.SecretReference) (IaaSClient, error)
}

type factory struct {
	StackitRegion       string
	StackitProjectID    string
	StackitAPIEndpoints config.APIEndpoints
}

func New(region, projectID string, apiEndpoints config.APIEndpoints) Factory {
	return &factory{
		StackitRegion:       region,
		StackitProjectID:    projectID,
		StackitAPIEndpoints: apiEndpoints,
	}
}

func (f factory) LoadBalancing(ctx context.Context) (LoadBalancingClient, error) {
	return NewLoadBalancingClient(ctx, f.StackitRegion, f.StackitProjectID, f.StackitAPIEndpoints)
}

func (f factory) IaaS(ctx context.Context, c client.Client, secretRef corev1.SecretReference) (IaaSClient, error) {
	credentials, err := stackit.GetCredentialsFromSecretRef(ctx, c, secretRef)
	if err != nil {
		return nil, err
	}

	return NewIaaSClient(f.StackitRegion, f.StackitAPIEndpoints, credentials)
}

//func clientOptions(endpoints config.APIEndpoints, credentials *stackit.Credentials) []sdkconfig.ConfigurationOption {
//	result := []sdkconfig.ConfigurationOption{
//		sdkconfig.WithUserAgent(UserAgent),
//		sdkconfig.WithServiceAccountKey(credentials.SaKeyJSON),
//	}
//
//	if endpoints.TokenEndpoint != nil {
//		result = append(result, sdkconfig.WithTokenEndpoint(*endpoints.TokenEndpoint))
//	}
//
//	return result
//}
