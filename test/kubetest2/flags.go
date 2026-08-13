package kubetest2

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
	"sigs.k8s.io/kubetest2/pkg/types"
)

func bindFlags(fs *pflag.FlagSet, d *Deployer) {
	fs.StringVar(&d.region, "region", defaultRegion, "STACKIT region for the SKE cluster")
	fs.StringVar(&d.kubernetesVersion, "kubernetes-version", "", "Kubernetes version for the SKE cluster")
	fs.StringVar(&d.availabilityZone, "availability-zone", defaultAvailabilityZone, "Availability zone for the SKE nodepool")
	fs.StringVar(&d.machineType, "machine-type", "", "Machine type for the SKE nodepool")
	fs.StringVar(&d.nodeImageName, "node-image-name", "", "Node image name for the SKE nodepool")
	fs.StringVar(&d.nodeImageVersion, "node-image-version", "", "Node image version for the SKE nodepool")
	fs.Int64Var(&d.nodeCount, "node-count", defaultNodeCount, "Node count for the SKE nodepool")
	fs.StringVar(&d.nodepoolName, "nodepool-name", defaultNodepoolName, "Nodepool name for the SKE cluster")
	fs.Int64Var(&d.volumeSizeGiB, "volume-size", defaultVolumeSizeGiB, "Root volume size in GiB for the SKE nodepool")
	fs.StringVar(&d.volumeType, "volume-type", "", "Root volume type for the SKE nodepool")
	fs.Int64Var(&d.kubeconfigExpiresIn, "kubeconfig-expiration-seconds", defaultKubeconfigExpiration, "Admin kubeconfig expiration in seconds")
}

func (d *Deployer) validate() error {
	klog.Infof(
		"Validating deployer configuration: run_id=%q region=%q kubernetes_version=%q availability_zone=%q machine_type=%q node_image_name=%q node_image_version=%q node_count=%d nodepool_name=%q volume_size=%d volume_type=%q kubeconfig_expiration_seconds=%d",
		d.options.RunID(),
		d.region,
		d.kubernetesVersion,
		d.availabilityZone,
		d.machineType,
		d.nodeImageName,
		d.nodeImageVersion,
		d.nodeCount,
		d.nodepoolName,
		d.volumeSizeGiB,
		d.volumeType,
		d.kubeconfigExpiresIn,
	)

	requiredFlags := map[string]string{
		"--region":             d.region,
		"--kubernetes-version": d.kubernetesVersion,
		"--availability-zone":  d.availabilityZone,
		"--machine-type":       d.machineType,
		"--node-image-name":    d.nodeImageName,
		"--node-image-version": d.nodeImageVersion,
	}

	for flagName, value := range requiredFlags {
		if strings.TrimSpace(value) == "" {
			return incorrectUsagef("%s is required", flagName)
		}
	}

	if strings.TrimSpace(d.nodepoolName) == "" {
		return incorrectUsagef("--nodepool-name must not be empty")
	}
	if len(d.nodepoolName) > 15 {
		return incorrectUsagef("--nodepool-name must be 15 characters or fewer")
	}
	if d.nodeCount < 1 {
		return incorrectUsagef("--node-count must be greater than 0")
	}
	if d.volumeSizeGiB < 1 {
		return incorrectUsagef("--volume-size must be greater than 0")
	}
	if d.kubeconfigExpiresIn < minKubeconfigExpiration || d.kubeconfigExpiresIn > maxKubeconfigExpiration {
		return incorrectUsagef(
			"--kubeconfig-expiration-seconds must be between %d and %d",
			minKubeconfigExpiration,
			maxKubeconfigExpiration,
		)
	}
	if strings.TrimSpace(d.options.RunID()) == "" {
		return incorrectUsagef("kubetest2 run-id must not be empty")
	}

	klog.Infof("Deployer configuration validation succeeded")

	return nil
}

func incorrectUsagef(format string, args ...any) error {
	return types.NewIncorrectUsage(fmt.Sprintf(format, args...))
}
