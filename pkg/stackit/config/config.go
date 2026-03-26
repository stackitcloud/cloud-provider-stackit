package config

import (
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/metadata"
)

type GlobalOpts struct {
	ProjectID    string       `yaml:"projectId"`
	Region       string       `yaml:"region"`
	APIEndpoints APIEndpoints `yaml:"apiEndpoints"`
}

type APIEndpoints struct {
	IaasAPI                               string `yaml:"iaasApi"`
	LoadBalancerAPI                       string `yaml:"loadBalancerApi"`
	ApplicationLoadBalancerAPI            string `yaml:"applicationLoadBalancerApi"`
	ApplicationLoadBalancerCertificateAPI string `yaml:"applicationLoadBalancerCertificateApi"`
}

type CCMConfig struct {
	Global       GlobalOpts       `yaml:"global"`
	Metadata     metadata.Opts    `yaml:"metadata"`
	LoadBalancer LoadBalancerOpts `yaml:"loadBalancer"`
}

type LoadBalancerOpts struct {
	NetworkID   string            `yaml:"networkId"`
	ExtraLabels map[string]string `yaml:"extraLabels"`
}

type CSIConfig struct {
	Global       GlobalOpts       `yaml:"global"`
	Metadata     metadata.Opts    `yaml:"metadata"`
	BlockStorage BlockStorageOpts `yaml:"blockStorage"`
}

type BlockStorageOpts struct {
	RescanOnResize bool `yaml:"rescanOnResize"`
}

type ALBConfig struct {
	Global                  GlobalOpts                  `yaml:"global"`
	Metadata                metadata.Opts               `yaml:"metadata"`
	ApplicationLoadBalancer ApplicationLoadBalancerOpts `yaml:"applicationLoadBalancer"`
}
type ApplicationLoadBalancerOpts struct {
	NetworkID string `yaml:"networkId"`
}
