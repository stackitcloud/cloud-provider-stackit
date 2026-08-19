package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/wait"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider/app"
	cloudcontrollerconfig "k8s.io/cloud-provider/app/config"
	"k8s.io/cloud-provider/names"
	"k8s.io/cloud-provider/options"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	"k8s.io/component-base/metrics/legacyregistry"
	_ "k8s.io/component-base/metrics/prometheus/clientgo"
	_ "k8s.io/component-base/metrics/prometheus/version"
	"k8s.io/klog/v2"

	_ "github.com/stackitcloud/cloud-provider-stackit/pkg/ccm"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/metrics"
)

func init() {
	legacyregistry.RawMustRegister(metrics.NewExporter())
}

func main() {
	ccmOptions, err := options.NewCloudControllerManagerOptions()
	if err != nil {
		klog.Fatalf("Unable to initialize command options: %v", err)
	}

	fmt.Println("starting Controller")
	controllerInitializers := app.DefaultInitFuncConstructors
	controllerAliases := names.CCMControllerAliases()

	additionalFlags := cliflag.NamedFlagSets{}

	// setup context
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	_ = additionalFlags.FlagSet("metrics").String("metrics-address", "", "set the prometheus metrics endpoint. Deprecated, do not use!")

	command := app.NewCloudControllerManagerCommand(ccmOptions, cloudInitializer(ctx), controllerInitializers, controllerAliases, additionalFlags, wait.NeverStop)
	pflag.CommandLine.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)
	logs.InitLogs()
	defer logs.FlushLogs()

	if err := command.ExecuteContext(ctx); err != nil {
		logs.FlushLogs()
		os.Exit(1) //nolint:gocritic // os.Exit(1) is executed before defer
	}
}

func cloudInitializer(_ context.Context) func(config *cloudcontrollerconfig.CompletedConfig) cloudprovider.Interface {
	return func(config *cloudcontrollerconfig.CompletedConfig) cloudprovider.Interface {
		cloudConfig := config.ComponentConfig.KubeCloudShared.CloudProvider
		// initialize cloud provider with the cloud provider name and config file provided
		cloud, err := cloudprovider.InitCloudProvider(cloudConfig.Name, cloudConfig.CloudConfigFile)
		if err != nil {
			klog.Fatalf("Cloud provider could not be initialized: %v", err)
		}
		if cloud == nil {
			klog.Fatalf("Cloud provider is nil")
		}

		if !cloud.HasClusterID() {
			if config.ComponentConfig.KubeCloudShared.AllowUntaggedCloud {
				klog.Warning("Detected a cluster without a ClusterID. A ClusterID will be required in the future. Please tag your cluster to avoid any future issues")
			} else {
				klog.Fatalf(
					"no ClusterID found. A ClusterID is required for the cloud provider to function properly. " +
						"This check can be bypassed by setting the allow-untagged-cloud option",
				)
			}
		}
		return cloud
	}
}
