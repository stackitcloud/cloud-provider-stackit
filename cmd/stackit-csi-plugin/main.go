package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/component-base/cli"
	"k8s.io/klog/v2"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/csi"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/csi/blockstorage"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/csi/util/mount"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/metadata"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/version"
)

var (
	endpoint                 string
	cloudConfig              string
	additionalTopologies     map[string]string
	cluster                  string
	httpEndpoint             string
	provideControllerService bool
	provideNodeService       bool
	withTopology             bool
)

func main() {
	cmd := &cobra.Command{
		Use:   "stackit-csi-plugin",
		Short: "STACKIT block-storage CSI plugin for SKE",
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

	cmd.PersistentFlags().BoolVar(&withTopology, "with-topology", true, "cluster is topology-aware")

	cmd.PersistentFlags().StringToStringVar(&additionalTopologies, "additional-topology", map[string]string{},
		"Additional CSI driver topology keys, for example topology.kubernetes.io/region=REGION1."+
			"This option can be specified multiple times to add multiple additional topology keys.")

	cmd.PersistentFlags().StringVar(&cluster, "cluster", "", "The identifier of the cluster that the plugin is running in.")
	cmd.PersistentFlags().StringVar(&httpEndpoint, "http-endpoint", "",
		"The TCP network address where the HTTP server for providing metrics for diagnostics, will listen (example: `:8080`)."+
			"The default is empty string, which means the server is disabled.")

	cmd.PersistentFlags().BoolVar(&provideControllerService, "provide-controller-service", true,
		"If set to true then the CSI driver does provide the controller service (default: true)")
	cmd.PersistentFlags().BoolVar(&provideNodeService, "provide-node-service", true,
		"If set to true then the CSI driver does provide the node service (default: true)")

	stackit.AddExtraFlags(pflag.CommandLine)

	code := cli.Run(cmd)
	os.Exit(code)
}

func handle() {
	// Initialize cloud
	d := blockstorage.NewDriver(&blockstorage.DriverOpts{
		Endpoint:     endpoint,
		ClusterID:    cluster,
		PVCLister:    csi.GetPVCLister(),
		WithTopology: withTopology,
	})

	if provideControllerService {
		var err error
		cfg, err := stackit.GetConfigForFile(cloudConfig)
		if err != nil {
			klog.Fatal(err)
		}

		iaasClient, err := stackit.CreateIAASClient(&cfg)
		if err != nil {
			klog.Fatalf("Failed to create IaaS client: %v", err)
		}

		stackitProvider, err := stackit.CreateSTACKITProvider(iaasClient, &cfg)
		if err != nil {
			klog.Fatalf("Failed to create STACKIT provider: %v", err)
		}

		d.SetupControllerService(stackitProvider)
	}

	if provideNodeService {
		// Initialize mount
		mountProvider := mount.GetMountProvider()

		cfg, err := stackit.GetConfigForFile(cloudConfig)
		if err != nil {
			klog.Fatal(err)
		}

		// Initialize Metadata
		metadataProvider := metadata.GetMetadataProvider(fmt.Sprintf("%s,%s", metadata.MetadataID, metadata.ConfigDriveID))

		d.SetupNodeService(mountProvider, metadataProvider, cfg.BlockStorage, additionalTopologies)
	}

	d.Run()
}
