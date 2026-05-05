package client

import (
	"errors"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
)

const (
	UserAgent = "cloud-provider-stackit"
)

var (
	ErrorNotFound = errors.New("not found")
)

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