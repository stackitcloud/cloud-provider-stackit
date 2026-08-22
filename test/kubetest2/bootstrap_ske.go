package kubetest2

import (
	"context"
	"fmt"
	"os"

	"github.com/stackitcloud/stackit-sdk-go/services/ske"
	"k8s.io/klog/v2"
)

// ensureCluster idempotently validates provider options, then creates or
// updates the SKE cluster, waits until it is ready and writes its kubeconfig.
// Depends on d.projectID and d.skeClient being set.
func (d *Deployer) ensureCluster(ctx context.Context) error {
	if err := d.validateProviderOptions(ctx); err != nil {
		return err
	}

	clusterName := d.clusterName()
	payload := d.clusterPayload()

	klog.Infof("Submitting SKE create/update request for cluster=%q", clusterName)
	if _, err := d.skeClient.CreateOrUpdateCluster(ctx, d.projectID, d.region, clusterName, payload); err != nil {
		return fmt.Errorf("create or update SKE cluster %q: %w", clusterName, err)
	}
	klog.Infof("Submitted SKE create/update request for cluster=%q", clusterName)

	if _, err := d.skeClient.WaitForClusterReady(ctx, d.projectID, d.region, clusterName); err != nil {
		return fmt.Errorf("wait for SKE cluster %q to become ready: %w", clusterName, err)
	}

	return d.writeKubeconfig(ctx, clusterName)
}

func (d *Deployer) writeKubeconfig(ctx context.Context, clusterName string) error {
	klog.Infof("Creating kubeconfig for cluster=%q", clusterName)
	kubeconfig, err := d.skeClient.CreateKubeconfig(ctx, d.projectID, d.region, clusterName, d.kubeconfigExpiresIn)
	if err != nil {
		return fmt.Errorf("create kubeconfig for SKE cluster %q: %w", clusterName, err)
	}
	klog.Infof("Writing kubeconfig for cluster=%q to %q", clusterName, d.kubeconfigPath)
	if err := os.WriteFile(d.kubeconfigPath, []byte(kubeconfig.GetKubeconfig()), 0o600); err != nil {
		return fmt.Errorf("write kubeconfig %q: %w", d.kubeconfigPath, err)
	}
	return nil
}

func (d *Deployer) validateProviderOptions(ctx context.Context) error {
	klog.Infof("Validating SKE provider options for region=%q", d.region)

	options, err := d.skeClient.ListProviderOptions(ctx, d.region)
	if err != nil {
		return fmt.Errorf("list SKE provider options: %w", err)
	}

	if !containsKubernetesVersion(options, d.kubernetesVersion) {
		return incorrectUsagef("unsupported --kubernetes-version %q for region %q", d.kubernetesVersion, d.region)
	}
	if !containsAvailabilityZone(options, d.availabilityZone) {
		return incorrectUsagef("unsupported --availability-zone %q for region %q", d.availabilityZone, d.region)
	}
	if !containsMachineType(options, d.machineType) {
		return incorrectUsagef("unsupported --machine-type %q for region %q", d.machineType, d.region)
	}
	if !containsMachineImage(options, d.nodeImageName, d.nodeImageVersion) {
		return incorrectUsagef(
			"unsupported node image %q version %q for region %q",
			d.nodeImageName,
			d.nodeImageVersion,
			d.region,
		)
	}
	if d.volumeType != "" && !containsVolumeType(options, d.volumeType) {
		return incorrectUsagef("unsupported --volume-type %q for region %q", d.volumeType, d.region)
	}

	klog.Infof("SKE provider option validation succeeded for region=%q", d.region)

	return nil
}

func (d *Deployer) clusterPayload() ske.CreateOrUpdateClusterPayload {
	klog.Infof(
		"Building SKE cluster payload: cluster=%q kubernetes_version=%q availability_zone=%q machine_type=%q image=%q/%q node_count=%d nodepool=%q volume_size=%d volume_type=%q",
		d.clusterName(),
		d.kubernetesVersion,
		d.availabilityZone,
		d.machineType,
		d.nodeImageName,
		d.nodeImageVersion,
		d.nodeCount,
		d.nodepoolName,
		d.volumeSizeGiB,
		d.volumeType,
	)

	clusterKubernetes := ske.NewKubernetes(d.kubernetesVersion)
	nodeImage := ske.NewImage(d.nodeImageName, d.nodeImageVersion)
	nodeMachine := ske.NewMachine(*nodeImage, d.machineType)
	nodeVolume := ske.NewVolume(d.volumeSizeGiB)
	if d.volumeType != "" {
		nodeVolume.SetType(d.volumeType)
	}

	nodepool := ske.NewNodepool(
		[]string{d.availabilityZone},
		*nodeMachine,
		d.nodeCount,
		d.nodeCount,
		d.nodepoolName,
		*nodeVolume,
	)
	nodepool.SetAllowSystemComponents(true)

	payload := ske.NewCreateOrUpdateClusterPayload(*clusterKubernetes, []ske.Nodepool{*nodepool})
	return *payload
}

func containsKubernetesVersion(options *ske.ProviderOptions, version string) bool {
	for _, item := range options.GetKubernetesVersions() {
		if item.GetVersion() == version {
			return true
		}
	}
	return false
}

func containsAvailabilityZone(options *ske.ProviderOptions, zone string) bool {
	for _, item := range options.GetAvailabilityZones() {
		if item.GetName() == zone {
			return true
		}
	}
	return false
}

func containsMachineType(options *ske.ProviderOptions, machineType string) bool {
	for _, item := range options.GetMachineTypes() {
		if item.GetName() == machineType {
			return true
		}
	}
	return false
}

func containsMachineImage(options *ske.ProviderOptions, imageName, imageVersion string) bool {
	for _, image := range options.GetMachineImages() {
		if image.GetName() != imageName {
			continue
		}
		for _, version := range image.GetVersions() {
			if version.GetVersion() == imageVersion {
				return true
			}
		}
	}
	return false
}

func containsVolumeType(options *ske.ProviderOptions, volumeType string) bool {
	for _, item := range options.GetVolumeTypes() {
		if item.GetName() == volumeType {
			return true
		}
	}
	return false
}
