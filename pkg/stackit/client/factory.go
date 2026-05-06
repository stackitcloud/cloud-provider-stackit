package client

import (
	"context"
	"errors"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
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
	LoadBalancing(context.Context, client.Client, corev1.SecretReference) (LoadBalancingClient, error)

	// IaaS returns a STACKIT IaaS service client.
	IaaS(context.Context, client.Client, corev1.SecretReference) (IaaSClient, error)
}

type factory struct {
	StackitRegion       string
	StackitAPIEndpoints stackitv1alpha1.APIEndpoints
}

func New(region string, cluster *extensionscontroller.Cluster) Factory {
	var apiEndpoints stackitv1alpha1.APIEndpoints

	if cloudProfileConfig, err := cloudProfileConfigFromCluster(cluster); err == nil {
		apiEndpoints = ptr.Deref(cloudProfileConfig.APIEndpoints, stackitv1alpha1.APIEndpoints{})
	}

	return &factory{
		StackitRegion:       region,
		StackitAPIEndpoints: apiEndpoints,
	}
}

func (f factory) LoadBalancing(ctx context.Context, c client.Client, secretRef corev1.SecretReference) (LoadBalancingClient, error) {
	credentials, err := stackit.GetCredentialsFromSecretRef(ctx, c, secretRef)
	if err != nil {
		return nil, err
	}

	return NewLoadBalancingClient(ctx, f.StackitRegion, f.StackitAPIEndpoints, credentials)
}

func (f factory) IaaS(ctx context.Context, c client.Client, secretRef corev1.SecretReference) (IaaSClient, error) {
	credentials, err := stackit.GetCredentialsFromSecretRef(ctx, c, secretRef)
	if err != nil {
		return nil, err
	}

	return NewIaaSClient(f.StackitRegion, f.StackitAPIEndpoints, credentials)
}

func clientOptions(endpoints stackitv1alpha1.APIEndpoints, credentials *stackit.Credentials) []sdkconfig.ConfigurationOption {
	result := []sdkconfig.ConfigurationOption{
		sdkconfig.WithUserAgent(UserAgent),
		sdkconfig.WithServiceAccountKey(credentials.SaKeyJSON),
	}

	if endpoints.TokenEndpoint != nil {
		result = append(result, sdkconfig.WithTokenEndpoint(*endpoints.TokenEndpoint))
	}

	return result
}
