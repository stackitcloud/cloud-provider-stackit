package client

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/spf13/pflag"
	stackitconfig "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/config"
	"gopkg.in/yaml.v3"
	"k8s.io/klog/v2"
)

func AddExtraFlags(fs *pflag.FlagSet) {
	fs.StringArrayVar(&userAgentData, "user-agent", nil, "Extra data to add to STACKIT SDK user-agent. Use multiple times to add more than one component.")
}

const (
	UserAgent = "cloud-provider-stackit"
)

// userAgentData is used to add extra information to the STACKIT SDK user-agent
var (
	userAgentData []string
	ErrorNotFound = errors.New("not found")
)


// Factory produces clients for various STACKIT services.
type Factory interface {
	// LoadBalancing returns a STACKIT load balancing service client.
	LoadBalancing(context.Context) (LoadBalancingClient, error)

	// IaaS returns a STACKIT IaaS service client.
	IaaS() (IaaSClient, error)
}

type factory struct {
	StackitRegion       string
	StackitProjectID    string
	StackitAPIEndpoints stackitconfig.APIEndpoints
}

func New(region, projectID string, apiEndpoints stackitconfig.APIEndpoints) Factory {
	return &factory{
		StackitRegion:       region,
		StackitProjectID:    projectID,
		StackitAPIEndpoints: apiEndpoints,
	}
}

func (f factory) LoadBalancing(ctx context.Context) (LoadBalancingClient, error) {
	return NewLoadBalancingClient(ctx, f.StackitRegion, f.StackitProjectID, f.StackitAPIEndpoints)
}

func (f factory) IaaS() (IaaSClient, error) {
	return NewIaaSClient(f.StackitRegion, f.StackitProjectID, f.StackitAPIEndpoints)
}

func GetConfigFromFile(path string) (stackitconfig.CSIConfig, error) {
	var cfg stackitconfig.CSIConfig

	config, err := os.Open(path)
	if err != nil {
		klog.ErrorS(err, "Failed to open stackitconfig file", "path", path)
		return cfg, err
	}
	defer config.Close()

	return GetConfig(config)
}

func GetConfig(reader io.Reader) (stackitconfig.CSIConfig, error) {
	var cfg stackitconfig.CSIConfig

	content, err := io.ReadAll(reader)
	if err != nil {
		klog.ErrorS(err, "Failed to read config content")
		return cfg, err
	}

	err = yaml.Unmarshal(content, &cfg)
	if err != nil {
		klog.ErrorS(err, "Failed to parse config as YAML")
		return cfg, err
	}

	return cfg, nil
}
