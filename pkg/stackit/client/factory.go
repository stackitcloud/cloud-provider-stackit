package client

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/pflag"
	stackitconfig "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/config"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/version"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	"gopkg.in/yaml.v3"
	"k8s.io/klog/v2"
)

func AddExtraFlags(fs *pflag.FlagSet) {
	fs.StringArrayVar(&userAgentData, "user-agent", nil, "Extra data to add to STACKIT SDK user-agent. Use multiple times to add more than one component.")
}

func BuildUserAgent(component, componentVersion string) string {
	userAgent := []string{fmt.Sprintf("%s/%s", component, componentVersion)}
	for _, data := range userAgentData {
		userAgent = append([]string{data}, userAgent...)
	}
	return strings.Join(userAgent, " ")
}

const defaultUserAgentComponent = "cloud-provider-stackit"

// userAgentData is used to add extra information to the STACKIT SDK user-agent
var (
	userAgentData []string
)

// Factory produces clients for various STACKIT services.
type Factory interface {
	// LoadBalancing returns a STACKIT load balancing service client.
	LoadBalancing(options []sdkconfig.ConfigurationOption) (LoadBalancingClient, error)

	// IaaS returns a STACKIT IaaS service client.
	IaaS(options []sdkconfig.ConfigurationOption) (IaaSClient, error)
}

type factory struct {
	StackitRegion    string
	StackitProjectID string
}

func New(region, projectID string) Factory {
	return &factory{
		StackitRegion:    region,
		StackitProjectID: projectID,
	}
}

func (f factory) LoadBalancing(options []sdkconfig.ConfigurationOption) (LoadBalancingClient, error) {
	return NewLoadBalancingClient(f.StackitRegion, f.StackitProjectID, withDefaultOptions(options))
}

func (f factory) IaaS(options []sdkconfig.ConfigurationOption) (IaaSClient, error) {
	return NewIaaSClient(f.StackitRegion, f.StackitProjectID, withDefaultOptions(options))
}

func withDefaultOptions(options []sdkconfig.ConfigurationOption) []sdkconfig.ConfigurationOption {
	return append(options,
		sdkconfig.WithUserAgent(BuildUserAgent(defaultUserAgentComponent, version.Version)))
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
