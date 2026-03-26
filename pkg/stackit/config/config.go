package config

import (
	"errors"
	"io"
	"os"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/metadata"
	"gopkg.in/yaml.v3"
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

func readFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return []byte{}, err
	}
	defer file.Close()

	return io.ReadAll(file)
}

func ReadALBConfigFromFile(path string) (ALBConfig, error) {
	content, err := readFile(path)
	if err != nil {
		return ALBConfig{}, err
	}

	config := ALBConfig{}
	err = yaml.Unmarshal(content, &config)
	if err != nil {
		return ALBConfig{}, err
	}

	if config.Global.ProjectID == "" {
		return ALBConfig{}, errors.New("project ID must be set")
	}
	if config.Global.Region == "" {
		return ALBConfig{}, errors.New("region must be set")
	}
	if config.ApplicationLoadBalancer.NetworkID == "" {
		return ALBConfig{}, errors.New("network ID must be set")
	}
	return config, nil
}
