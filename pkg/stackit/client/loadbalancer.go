package client

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/stackiterrors"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	"github.com/stackitcloud/stackit-sdk-go/core/runtime"
	sdkWait "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api/wait"
	loadbalancer "github.com/stackitcloud/stackit-sdk-go/services/loadbalancer/v2api"
)

type LoadBalancingClient interface {
	CreateLoadBalancer(ctx context.Context, payload *loadbalancer.CreateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error)
	GetLoadBalancer(ctx context.Context, id string) (*loadbalancer.LoadBalancer, error)
	UpdateLoadBalancer(ctx context.Context, lbName string, updates *loadbalancer.UpdateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error)
	DeleteLoadBalancer(ctx context.Context, lbName string) error
	UpdateTargetPool(ctx context.Context, name, targetPoolName string, payload loadbalancer.UpdateTargetPoolPayload) error

	CreateCredentials(ctx context.Context, payload loadbalancer.CreateCredentialsPayload) (*loadbalancer.CreateCredentialsResponse, error)
	ListCredentials(ctx context.Context) (*loadbalancer.ListCredentialsResponse, error)
	UpdateCredentials(ctx context.Context, credentialsRef string, payload loadbalancer.UpdateCredentialsPayload) error
	DeleteCredentials(ctx context.Context, credentialsRef string) error
}

type loadBalancingClient struct {
	Client    loadbalancer.DefaultAPI
	projectID string
	region    string
}

func NewLoadBalancingClient(region, projectID string, options []sdkconfig.ConfigurationOption) (LoadBalancingClient, error) {
	apiClient, err := loadbalancer.NewAPIClient(options...)
	if err != nil {
		return nil, err
	}
	return &loadBalancingClient{
		Client:    apiClient.DefaultAPI,
		projectID: projectID,
		region:    region,
	}, nil
}

func (l *loadBalancingClient) CreateLoadBalancer(ctx context.Context, payload *loadbalancer.CreateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	lb, err := l.Client.
		CreateLoadBalancer(ctx, l.projectID, l.region).
		CreateLoadBalancerPayload(*payload).
		XRequestID(uuid.NewString()).
		Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return lb, nil
}

func (l *loadBalancingClient) DeleteLoadBalancer(ctx context.Context, lbName string) error {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	_, err := l.Client.
		DeleteLoadBalancer(ctx, l.projectID, l.region, lbName).
		Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return err
	}

	return nil
}

func (l *loadBalancingClient) GetLoadBalancer(ctx context.Context, lbName string) (*loadbalancer.LoadBalancer, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	lb, err := l.Client.
		GetLoadBalancer(ctx, l.projectID, l.region, lbName).
		Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return lb, nil
}

func (l *loadBalancingClient) UpdateLoadBalancer(ctx context.Context, lbName string, updates *loadbalancer.UpdateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	lb, err := l.Client.
		UpdateLoadBalancer(ctx, l.projectID, l.region, lbName).
		UpdateLoadBalancerPayload(*updates).
		Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return lb, nil
}

func (l *loadBalancingClient) UpdateTargetPool(ctx context.Context, name, targetPoolName string, payload loadbalancer.UpdateTargetPoolPayload) error {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	_, err := l.Client.
		UpdateTargetPool(ctx, l.projectID, l.region, name, targetPoolName).
		UpdateTargetPoolPayload(payload).
		Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return err
	}

	return nil
}

func (l *loadBalancingClient) CreateCredentials(ctx context.Context, payload loadbalancer.CreateCredentialsPayload) (*loadbalancer.CreateCredentialsResponse, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	resp, err := l.Client.
		CreateCredentials(ctx, l.projectID, l.region).
		CreateCredentialsPayload(payload).
		XRequestID(uuid.NewString()).
		Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return resp, nil
}

func (l *loadBalancingClient) ListCredentials(ctx context.Context) (*loadbalancer.ListCredentialsResponse, error) {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	resp, err := l.Client.
		ListCredentials(ctx, l.projectID, l.region).
		Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return nil, stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return nil, err
	}

	return resp, nil
}

func (l *loadBalancingClient) UpdateCredentials(ctx context.Context, credentialsRef string, payload loadbalancer.UpdateCredentialsPayload) error {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	_, err := l.Client.
		UpdateCredentials(ctx, l.projectID, l.region, credentialsRef).
		UpdateCredentialsPayload(payload).
		Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return err
	}

	return nil
}

func (l *loadBalancingClient) DeleteCredentials(ctx context.Context, credentialsRef string) error {
	var httpResp *http.Response
	ctx = runtime.WithCaptureHTTPResponse(ctx, &httpResp)

	_, err := l.Client.
		DeleteCredentials(ctx, l.projectID, l.region, credentialsRef).
		Execute()
	if err != nil {
		if httpResp != nil {
			reqID := httpResp.Header.Get(sdkWait.XRequestIDHeader)
			return stackiterrors.WrapErrorWithResponseID(err, reqID)
		}

		return err
	}

	return nil
}
