package stackit

import (
	"context"

	corev1 "k8s.io/api/core/v1"
)

// LoadBalancer is used for creating and maintaining load balancers
type LoadBalancer struct{}

//nolint:golint,all // should be implemented
func (lb *LoadBalancer) GetLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service) (
	status *corev1.LoadBalancerStatus, exists bool, err error) {
	return nil, false, nil
}

//nolint:golint,all // should be implemented
func (lb *LoadBalancer) GetLoadBalancerName(ctx context.Context, clusterName string, service *corev1.Service) string {
	return ""
}

//nolint:golint,all // should be implemented
func (lb *LoadBalancer) EnsureLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) (
	*corev1.LoadBalancerStatus, error) {
	return nil, nil
}

//nolint:golint,all // should be implemented
func (lb *LoadBalancer) UpdateLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) error {
	return nil
}

//nolint:golint,all // should be implemented
func (lb *LoadBalancer) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *corev1.Service) error {
	return nil
}
