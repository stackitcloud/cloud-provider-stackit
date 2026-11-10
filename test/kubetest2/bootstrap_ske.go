package kubetest2

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Masterminds/semver/v3"
	ske "github.com/stackitcloud/stackit-sdk-go/services/ske/v2api"
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
	content := kubeconfig.GetKubeconfig()
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("create kubeconfig for SKE cluster %q returned empty kubeconfig", clusterName)
	}
	if err := os.WriteFile(d.kubeconfigPath, []byte(content), 0o600); err != nil {
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
	if options == nil {
		return fmt.Errorf("list SKE provider options returned nil response")
	}

	resolvedKubernetesVersion, err := resolveKubernetesVersion(options, d.kubernetesVersion, d.kubernetesVersionState, d.useLatestKubernetesVersion)
	if err != nil {
		return incorrectUsagef(
			"unsupported --kubernetes-version %q (state=%q) for region %q: %v",
			d.kubernetesVersion,
			d.kubernetesVersionState,
			d.region,
			err,
		)
	}
	d.kubernetesVersion = resolvedKubernetesVersion

	if !containsAvailabilityZone(options, d.availabilityZone) {
		return incorrectUsagef("unsupported --availability-zone %q for region %q", d.availabilityZone, d.region)
	}
	if !containsMachineType(options, d.machineType) {
		return incorrectUsagef("unsupported --machine-type %q for region %q", d.machineType, d.region)
	}
	resolvedImageVersion, err := resolveMachineImageVersion(options, d.nodeImageName, d.nodeImageVersion, d.nodeImageVersionState)
	if err != nil {
		return incorrectUsagef(
			"unsupported node image %q version %q (state=%q) for region %q: %v",
			d.nodeImageName,
			d.nodeImageVersion,
			d.nodeImageVersionState,
			d.region,
			err,
		)
	}
	d.nodeImageVersion = resolvedImageVersion

	if d.volumeType != "" && !containsVolumeType(options, d.volumeType) {
		return incorrectUsagef("unsupported --volume-type %q for region %q", d.volumeType, d.region)
	}

	klog.Infof(
		"SKE provider option validation succeeded for region=%q: kubernetes_version=%q (state=%q) node_image=%q/%q (state=%q)",
		d.region,
		d.kubernetesVersion,
		d.kubernetesVersionState,
		d.nodeImageName,
		d.nodeImageVersion,
		d.nodeImageVersionState,
	)

	return nil
}

func (d *Deployer) clusterPayload() *ske.CreateOrUpdateClusterPayload {
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
	return payload
}

func matchesVersionState(candidateState, desiredState string) bool {
	candidateState = strings.TrimSpace(candidateState)
	if candidateState == "" {
		return strings.EqualFold(desiredState, defaultVersionState)
	}
	return strings.EqualFold(candidateState, desiredState)
}

func resolveKubernetesVersion(options *ske.ProviderOptions, kubernetesVersion, state string, useLatest bool) (string, error) {
	versions := options.GetKubernetesVersions()
	var requestedVersion *semver.Version
	if !useLatest {
		exactVersion, parsedVersion, err := parseRequestedKubernetesVersion(versions, kubernetesVersion, state)
		if err != nil || exactVersion != "" {
			return exactVersion, err
		}
		requestedVersion = parsedVersion
	}

	return findBestKubernetesVersion(versions, requestedVersion, state)
}

func parseRequestedKubernetesVersion(versions []ske.KubernetesVersion, kubernetesVersion, state string) (string, *semver.Version, error) {
	kubernetesVersion = strings.TrimPrefix(strings.TrimSpace(kubernetesVersion), "v")
	if kubernetesVersion == "" {
		return "", nil, fmt.Errorf("kubernetes version must not be empty")
	}

	if exactVersion, ok := findExactKubernetesVersion(versions, kubernetesVersion, state); ok {
		return exactVersion, nil, nil
	}
	if strings.Count(kubernetesVersion, ".") != 1 {
		return "", nil, fmt.Errorf("exact kubernetes version %q with state %q not found", kubernetesVersion, state)
	}
	requestedVersion, err := semver.NewVersion(kubernetesVersion)
	if err != nil {
		return "", nil, fmt.Errorf("parse kubernetes version %q: %w", kubernetesVersion, err)
	}
	if requestedVersion.Prerelease() != "" || requestedVersion.Metadata() != "" {
		return "", nil, fmt.Errorf("kubernetes minor version %q must not contain prerelease or metadata", kubernetesVersion)
	}
	return "", requestedVersion, nil
}

func findBestKubernetesVersion(versions []ske.KubernetesVersion, requestedVersion *semver.Version, state string) (string, error) {
	var bestVersion *semver.Version
	for _, item := range versions {
		if !matchesVersionState(item.GetState(), state) {
			continue
		}
		candidateVersion, err := semver.NewVersion(item.GetVersion())
		if err != nil {
			continue
		}
		if requestedVersion != nil && (candidateVersion.Major() != requestedVersion.Major() || candidateVersion.Minor() != requestedVersion.Minor()) {
			continue
		}
		if bestVersion == nil || candidateVersion.GreaterThan(bestVersion) {
			bestVersion = candidateVersion
		}
	}
	if bestVersion == nil {
		if requestedVersion != nil {
			return "", fmt.Errorf("no kubernetes version matching %d.%d with state %q found", requestedVersion.Major(), requestedVersion.Minor(), state)
		}
		return "", fmt.Errorf("no kubernetes version with state %q found", state)
	}
	return bestVersion.Original(), nil
}

func findExactKubernetesVersion(versions []ske.KubernetesVersion, kubernetesVersion, state string) (string, bool) {
	for _, item := range versions {
		if !matchesVersionState(item.GetState(), state) {
			continue
		}
		if item.GetVersion() == kubernetesVersion {
			return item.GetVersion(), true
		}
	}
	return "", false
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

func resolveMachineImageVersion(options *ske.ProviderOptions, imageName, imageVersion, state string) (string, error) {
	imageVersion = strings.TrimSpace(imageVersion)
	for _, image := range options.GetMachineImages() {
		if image.GetName() == imageName {
			return resolveSingleMachineImageVersion(image, imageVersion, state)
		}
	}
	return "", fmt.Errorf("machine image %q not found", imageName)
}

func resolveSingleMachineImageVersion(image ske.MachineImage, imageVersion, state string) (string, error) {
	var requestedVersion *semver.Version
	if imageVersion != "" {
		exactVersion, parsedVersion, err := parseRequestedMachineImageVersion(image, imageVersion, state)
		if err != nil || exactVersion != "" {
			return exactVersion, err
		}
		requestedVersion = parsedVersion
	}

	return findBestMachineImageVersion(image, requestedVersion, state)
}

func parseRequestedMachineImageVersion(image ske.MachineImage, imageVersion, state string) (string, *semver.Version, error) {
	if exactVersion, ok := findExactMachineImageVersion(image.GetVersions(), imageVersion, state); ok {
		return exactVersion, nil, nil
	}
	if strings.Contains(imageVersion, ".") {
		return "", nil, fmt.Errorf("exact version %q with state %q not found for image %q", imageVersion, state, image.GetName())
	}
	requestedVersion, err := semver.NewVersion(imageVersion)
	if err != nil {
		return "", nil, fmt.Errorf("parse image version %q: %w", imageVersion, err)
	}
	if requestedVersion.Prerelease() != "" || requestedVersion.Metadata() != "" {
		return "", nil, fmt.Errorf("image major version %q must not contain prerelease or metadata", imageVersion)
	}
	return "", requestedVersion, nil
}

func findExactMachineImageVersion(versions []ske.MachineImageVersion, imageVersion, state string) (string, bool) {
	for _, version := range versions {
		if matchesVersionState(version.GetState(), state) && version.GetVersion() == imageVersion {
			return version.GetVersion(), true
		}
	}
	return "", false
}

func findBestMachineImageVersion(image ske.MachineImage, requestedVersion *semver.Version, state string) (string, error) {
	var bestVersion *semver.Version
	for _, version := range image.GetVersions() {
		if !matchesVersionState(version.GetState(), state) {
			continue
		}
		candidateVersion, err := semver.NewVersion(version.GetVersion())
		if err != nil {
			continue
		}
		if requestedVersion != nil && candidateVersion.Major() != requestedVersion.Major() {
			continue
		}
		if bestVersion == nil || compareMachineImageVersions(candidateVersion, bestVersion) > 0 {
			bestVersion = candidateVersion
		}
	}
	if bestVersion == nil {
		if requestedVersion != nil {
			return "", fmt.Errorf("no version matching major %d with state %q found for image %q", requestedVersion.Major(), state, image.GetName())
		}
		return "", fmt.Errorf("no version with state %q found for image %q", state, image.GetName())
	}
	return bestVersion.Original(), nil
}

// compareMachineImageVersions compares two machine image versions.
// By default in SemVer, a suffix like "-containerd2.1.9" is treated as a pre-release
// and would rank lower than the same base version without a suffix (e.g. "4593.2.5").
// During the Flatcar transition, SKE offers both legacy unsuffixed images and
// "-containerd<version>" images; we explicitly prefer "-containerd" flavors over
// unsuffixed ones for the same base version and compare the embedded containerd
// version using SemVer when both images carry the suffix.
func compareMachineImageVersions(versionA, versionB *semver.Version) int {
	baseVersionA := semver.New(versionA.Major(), versionA.Minor(), versionA.Patch(), "", "")
	baseVersionB := semver.New(versionB.Major(), versionB.Minor(), versionB.Patch(), "", "")
	if comparison := baseVersionA.Compare(baseVersionB); comparison != 0 {
		return comparison
	}

	containerdVersionA, hasContainerdA := strings.CutPrefix(versionA.Prerelease(), "containerd")
	containerdVersionB, hasContainerdB := strings.CutPrefix(versionB.Prerelease(), "containerd")
	if hasContainerdA != hasContainerdB {
		if hasContainerdA {
			return 1
		}
		return -1
	}
	if hasContainerdA && hasContainerdB {
		parsedContainerdA, errA := semver.NewVersion(containerdVersionA)
		parsedContainerdB, errB := semver.NewVersion(containerdVersionB)
		if errA == nil && errB == nil {
			return parsedContainerdA.Compare(parsedContainerdB)
		}
	}

	return versionA.Compare(versionB)
}

func containsVolumeType(options *ske.ProviderOptions, volumeType string) bool {
	for _, item := range options.GetVolumeTypes() {
		if item.GetName() == volumeType {
			return true
		}
	}
	return false
}
