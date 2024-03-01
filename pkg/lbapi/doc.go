//go:generate mockgen -destination=mocks.go -package lbapi github.com/stackitcloud/cloud-provider-stackit/pkg/lbapi Client

// lbapi is a small wrapper around the load balancer SDK to simplify the interface and provide a mock for testing.
package lbapi
