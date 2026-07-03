package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/version"
	"k8s.io/component-base/cli"
	"k8s.io/klog/v2"
)

func main() {
	cmd := &cobra.Command{
		Use:   "stackit-credential-provider",
		Short: "STACKIT credential provider for kubelet",
		RunE: func(cmd *cobra.Command, args []string) error {
			provider, err := NewProvider()
			if err != nil {
				return err
			}
			p := NewCredentialPlugin(provider)
			klog.Info("running plugin")
			if err := p.Run(cmd.Context(), args); err != nil {
				return fmt.Errorf("error running credential provider plugin: %w", err)
			}
			return nil
		},
		Version: version.Version,
	}

	stackit.AddExtraFlags(pflag.CommandLine)

	os.Exit(cli.Run(cmd))
}
