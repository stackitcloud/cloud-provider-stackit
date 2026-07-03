package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stackitcloud/stackit-sdk-go/core/auth"
	"github.com/stackitcloud/stackit-sdk-go/core/clients"
	"github.com/stackitcloud/stackit-sdk-go/core/config"
	"k8s.io/klog/v2"
	v1 "k8s.io/kubelet/pkg/apis/credentialprovider/v1"
)

type tokenGetter interface {
	GetAccessToken() (string, error)
}

var _ CredentialProvider = (*provider)(nil)

type provider struct {
	tokenGetter tokenGetter
}

func NewProvider() (*provider, error) {
	var getter tokenGetter
	rt, err := auth.DefaultAuth(&config.Configuration{})
	if err == nil {
		authFlow, ok := rt.(clients.AuthFlow)
		if !ok {
			return nil, errors.New("cannot get auth flow")
		}
		getter = authFlow
	} else {
		klog.Info("using server flow")
		getter = &serverFlow{}
	}
	return &provider{
		tokenGetter: getter,
	}, nil
}

// GetCredentials implements [CredentialProvider].
func (p *provider) GetCredentials(ctx context.Context, request *v1.CredentialProviderRequest, args []string) (response *v1.CredentialProviderResponse, err error) {
	imageHost, err := parseHostFromImageReference(request.Image)
	if err != nil {
		return nil, err
	}

	token, err := p.tokenGetter.GetAccessToken()
	if err != nil {
		return nil, err
	}
	serviceAccountEmail, err := emailFromToken(token)
	if err != nil {
		return nil, err
	}

	return &v1.CredentialProviderResponse{
		Auth: map[string]v1.AuthConfig{
			imageHost: {
				Username: serviceAccountEmail,
				Password: token,
			},
		},
		// service accounts in registry are project based, therefore we cannot cache by registry
		CacheKeyType: v1.ImagePluginCacheKeyType,
	}, nil
}

func emailFromToken(tokenStr string) (string, error) {
	claims := jwt.MapClaims{}
	_, _, err := jwt.NewParser().ParseUnverified(tokenStr, claims)
	if err != nil {
		return "", err
	}
	return claims["email"].(string), nil
}

// parseHostFromImageReference parses the hostname from an image reference
func parseHostFromImageReference(image string) (string, error) {
	// a URL needs a scheme to be parsed correctly
	if !strings.Contains(image, "://") {
		image = "https://" + image
	}
	parsed, err := url.Parse(image)
	if err != nil {
		return "", fmt.Errorf("error parsing image reference %s: %v", image, err)
	}
	return parsed.Hostname(), nil
}
