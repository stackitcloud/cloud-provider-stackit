package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/csi"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/csi/blockstorage"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/csi/util/mount"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/metrics"
	stackitclient "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/client"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/metadata"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/version"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	"k8s.io/component-base/cli"
	"k8s.io/klog/v2"
)

var (
	endpoint                 string
	cloudConfig              string
	cluster                  string
	httpEndpoint             string
	provideControllerService bool
	provideNodeService       bool
	legacyStorageMode        bool
	legacyVolumeCreation     bool
)

func main() {
	cmd := &cobra.Command{
		Use:   "stackit-csi-plugin",
		Short: "STACKIT block-storage CSI plugin",
		Run: func(_ *cobra.Command, _ []string) {
			handle()
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			f := cmd.Flags()

			if !provideControllerService {
				return nil
			}

			configs, err := f.GetString("cloud-config")
			if err != nil {
				return err
			}

			if configs == "" {
				return fmt.Errorf("unable to mark flag cloud-config to be required")
			}

			return nil
		},
		Version: version.Version,
	}

	csi.AddPVCFlags(cmd)

	cmd.PersistentFlags().StringVar(&endpoint, "endpoint", "", "CSI endpoint")
	if err := cmd.MarkPersistentFlagRequired("endpoint"); err != nil {
		klog.Fatalf("Unable to mark flag endpoint to be required: %v", err)
	}

	cmd.Flags().StringVar(&cloudConfig, "cloud-config", "", "CSI driver cloud config. This option can be given multiple times")

	cmd.PersistentFlags().StringVar(&cluster, "cluster", "", "The identifier of the cluster that the plugin is running in.")
	cmd.PersistentFlags().StringVar(&httpEndpoint, "http-endpoint", "",
		"The TCP network address where the HTTP server for providing metrics for diagnostics, will listen (example: `:8080`)."+
			"The default is empty string, which means the server is disabled.")

	cmd.PersistentFlags().BoolVar(&provideControllerService, "provide-controller-service", true,
		"If set to true then the CSI driver does provide the controller service (default: true)")
	cmd.PersistentFlags().BoolVar(&provideNodeService, "provide-node-service", true,
		"If set to true then the CSI driver does provide the node service (default: true)")
	cmd.PersistentFlags().BoolVar(&legacyStorageMode, "legacy-storage-mode", false,
		"Configures the CSI to listen to the legacy storage driverName cinder.csi.openstack.org instead")
	cmd.PersistentFlags().BoolVar(&legacyVolumeCreation, "legacy-volume-creation", true, "Enable or disable support for creating volumes with the old driverName (cinder.csi.openstack.org)")

	stackitclient.AddExtraFlags(pflag.CommandLine)

	code := cli.Run(cmd)
	os.Exit(code)
}

func handle() {
	// Initialize cloud
	driverOpts := &blockstorage.DriverOpts{
		Endpoint:  endpoint,
		ClusterID: cluster,
		PVCLister: csi.GetPVCLister(),
	}

	if legacyStorageMode {
		driverOpts.LegacyDriverName = true
	}

	if !legacyVolumeCreation {
		driverOpts.BlockVolumeCreation = true
	}

	d := blockstorage.NewDriver(driverOpts)

	if provideControllerService {
		var err error
		cfg, err := stackitclient.GetConfigFromFile(cloudConfig)
		if err != nil {
			klog.Fatal(err)
		}

		iaasOpts := []sdkconfig.ConfigurationOption{
			sdkconfig.WithHTTPClient(metrics.NewInstrumentedHTTPClient()),
		}

		if cfg.Global.APIEndpoints.IaasAPI != "" {
			iaasOpts = append(iaasOpts, sdkconfig.WithEndpoint(cfg.Global.APIEndpoints.IaasAPI))
		}

		iaasClient, err := stackitclient.New(cfg.Global.Region, cfg.Global.ProjectID, "", "").IaaS(iaasOpts)
		if err != nil {
			klog.Fatalf("Failed to create STACKIT provider: %v", err)
		}

		d.SetupControllerService(iaasClient)
	}

	if provideNodeService {
		// Initialize mount
		mountProvider := mount.GetMountProvider()

		cfg, err := stackitclient.GetConfigFromFile(cloudConfig)
		if err != nil {
			klog.Fatal(err)
		}

		// Initialize Metadata
		metadataProvider := metadata.GetMetadataProvider(fmt.Sprintf("%s,%s", metadata.MetadataID, metadata.ConfigDriveID))

		d.SetupNodeService(mountProvider, metadataProvider, cfg.BlockStorage)
	}

	d.Run()
}
