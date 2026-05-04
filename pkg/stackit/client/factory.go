package client

import (
	"errors"
	"net/http"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	oapiError "github.com/stackitcloud/stackit-sdk-go/core/oapierror"
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

func isOpenAPINotFound(err error) bool {
	apiErr := &oapiError.GenericOpenAPIError{}
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == http.StatusNotFound
}

func IsNotFound(err error) bool {
	return errors.Is(err, ErrorNotFound)
}
