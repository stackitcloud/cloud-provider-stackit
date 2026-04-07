package config

import (
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/metadata"
)

type GlobalOpts struct {
	ProjectID      string       `yaml:"projectId"`
	AreaID         string       `yaml:"areaId"`
	OrganizationID string       `yaml:"organizationId"`
	Region         string       `yaml:"region"`
	APIEndpoints   APIEndpoints `yaml:"apiEndpoints"`
}

type APIEndpoints struct {
	IaasAPI         string `yaml:"iaasApi"`
	LoadBalancerAPI string `yaml:"loadBalancerApi"`
}

type CCMConfig struct {
	Global       GlobalOpts       `yaml:"global"`
	Metadata     metadata.Opts    `yaml:"metadata"`
	LoadBalancer LoadBalancerOpts `yaml:"loadBalancer"`
	Instance     InstanceOpts     `yaml:"instance"`
	Route        RouteOpts        `yaml:"route"`
}

type InstanceOpts struct {
	// DefaultNetwork contains the default network to use for a node.
	// It can contain either the network name or ID.
	// Can be used in mulit-network scenario to indicate which NIC is the primary one.
	DefaultNetwork string `yaml:"defaultNetwork"`
}

type LoadBalancerOpts struct {
	NetworkID   string            `yaml:"networkId"`
	ExtraLabels map[string]string `yaml:"extraLabels"`
}

type RouteOpts struct {
	RoutingTableID string `yaml:"routingTableId"`
}

type CSIConfig struct {
	Global       GlobalOpts       `yaml:"global"`
	Metadata     metadata.Opts    `yaml:"metadata"`
	BlockStorage BlockStorageOpts `yaml:"blockStorage"`
}

type BlockStorageOpts struct {
	RescanOnResize bool `yaml:"rescanOnResize"`
}
