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
	if d.options != nil && d.options.ShouldTest() && d.testPackageAcquirer != nil {
		if err := d.testPackageAcquirer(ctx, d.kubernetesVersion, d.options.RunDir()); err != nil {
			return fmt.Errorf("acquire test package: %w", err)
		}
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

	versionState := d.effectiveVersionState()

	resolvedK8sVersion, ok := resolveKubernetesVersion(options, d.kubernetesVersion, versionState)
	if !ok {
		return incorrectUsagef("unsupported --kubernetes-version %q (state=%q) for region %q", d.kubernetesVersion, versionState, d.region)
	}
	d.kubernetesVersion = resolvedK8sVersion

	if !containsAvailabilityZone(options, d.availabilityZone) {
		return incorrectUsagef("unsupported --availability-zone %q for region %q", d.availabilityZone, d.region)
	}
	if !containsMachineType(options, d.machineType) {
		return incorrectUsagef("unsupported --machine-type %q for region %q", d.machineType, d.region)
	}
	resolvedImageVersion, ok := resolveMachineImageVersion(options, d.nodeImageName, d.nodeImageVersion, versionState)
	if !ok {
		return incorrectUsagef(
			"unsupported node image %q version %q (state=%q) for region %q",
			d.nodeImageName,
			d.nodeImageVersion,
			versionState,
			d.region,
		)
	}
	d.nodeImageVersion = resolvedImageVersion

	if d.volumeType != "" && !containsVolumeType(options, d.volumeType) {
		return incorrectUsagef("unsupported --volume-type %q for region %q", d.volumeType, d.region)
	}

	klog.Infof(
		"SKE provider option validation succeeded for region=%q: kubernetes_version=%q node_image=%q/%q state=%q",
		d.region,
		d.kubernetesVersion,
		d.nodeImageName,
		d.nodeImageVersion,
		versionState,
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

func (d *Deployer) effectiveVersionState() string {
	state := strings.ToLower(strings.TrimSpace(d.versionState))
	if state == "" {
		return defaultVersionState
	}
	return state
}

func matchesVersionState(candidateState, desiredState string) bool {
	trimmed := strings.TrimSpace(candidateState)
	if trimmed == "" {
		return strings.EqualFold(desiredState, defaultVersionState)
	}
	return strings.EqualFold(trimmed, desiredState)
}

func resolveKubernetesVersion(options *ske.ProviderOptions, requested, state string) (string, bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(requested), "v")
	if trimmed == "" {
		return "", false
	}

	if v, ok := findExactKubernetesVersion(options.GetKubernetesVersions(), requested, trimmed, state); ok {
		return v, true
	}
	if strings.Count(trimmed, ".") != 1 {
		return "", false
	}
	reqVer, err := semver.NewVersion(trimmed)
	if err != nil || reqVer.Prerelease() != "" || reqVer.Metadata() != "" {
		return "", false
	}
	return latestMinorKubernetesVersion(options.GetKubernetesVersions(), reqVer.Major(), reqVer.Minor(), state)
}

func findExactKubernetesVersion(versions []ske.KubernetesVersion, requested, trimmed, state string) (string, bool) {
	for _, item := range versions {
		if !matchesVersionState(item.GetState(), state) {
			continue
		}
		if item.GetVersion() == trimmed || item.GetVersion() == requested {
			return item.GetVersion(), true
		}
	}
	return "", false
}

func latestMinorKubernetesVersion(versions []ske.KubernetesVersion, major, minor uint64, state string) (string, bool) {
	var (
		bestVersion string
		bestSemver  *semver.Version
	)
	for _, item := range versions {
		if !matchesVersionState(item.GetState(), state) {
			continue
		}
		candVer, err := semver.NewVersion(item.GetVersion())
		if err != nil || candVer.Major() != major || candVer.Minor() != minor {
			continue
		}
		if bestSemver == nil || candVer.GreaterThan(bestSemver) {
			bestSemver = candVer
			bestVersion = item.GetVersion()
		}
	}
	return bestVersion, bestSemver != nil
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

func resolveMachineImageVersion(options *ske.ProviderOptions, imageName, requested, state string) (string, bool) {
	trimmedRequested := strings.TrimSpace(requested)
	for _, image := range options.GetMachineImages() {
		if image.GetName() == imageName {
			return resolveSingleMachineImageVersion(image, trimmedRequested, state)
		}
	}
	return "", false
}

func resolveSingleMachineImageVersion(image ske.MachineImage, requested, state string) (string, bool) {
	if requested != "" {
		for _, version := range image.GetVersions() {
			if matchesVersionState(version.GetState(), state) && version.GetVersion() == requested {
				return version.GetVersion(), true
			}
		}
		if !strings.EqualFold(image.GetName(), "ubuntu") || !isUbuntuMajorShorthand(requested) {
			return "", false
		}
	}

	var best string
	for _, version := range image.GetVersions() {
		if !matchesVersionState(version.GetState(), state) {
			continue
		}
		cand := version.GetVersion()
		if requested != "" && !matchesUbuntuMajor(cand, requested) {
			continue
		}
		if best == "" || compareMachineImageVersions(cand, best) > 0 {
			best = cand
		}
	}
	return best, best != ""
}

func isUbuntuMajorShorthand(requested string) bool {
	normalized := strings.ReplaceAll(strings.TrimSpace(requested), ".", "")
	if len(normalized) != 2 && len(normalized) != 4 {
		return false
	}
	for i := range len(normalized) {
		if normalized[i] < '0' || normalized[i] > '9' {
			return false
		}
	}
	return true
}

func matchesUbuntuMajor(candidateVersion, requested string) bool {
	normalized := strings.ReplaceAll(strings.TrimSpace(requested), ".", "")
	majorPart, _, ok := strings.Cut(candidateVersion, ".")
	if !ok {
		return false
	}
	if len(normalized) == 2 {
		return len(majorPart) == 4 && strings.HasPrefix(majorPart, normalized)
	}
	return majorPart == normalized
}

func compareMachineImageVersions(a, b string) int {
	baseAStr, criAStr, hasCRIA := strings.Cut(a, "-containerd")
	baseBStr, criBStr, hasCRIB := strings.Cut(b, "-containerd")

	baseA, errA := semver.NewVersion(baseAStr)
	baseB, errB := semver.NewVersion(baseBStr)
	if errA == nil && errB == nil {
		if cmp := baseA.Compare(baseB); cmp != 0 {
			return cmp
		}
		// shortcut: Flatcar transition offers both legacy and -containerd2.x images; remove once legacy flavors are dropped.
		if hasCRIA != hasCRIB {
			if hasCRIA {
				return 1
			}
			return -1
		}
		if hasCRIA && hasCRIB {
			criA, errCRIA := semver.NewVersion(criAStr)
			criB, errCRIB := semver.NewVersion(criBStr)
			if errCRIA == nil && errCRIB == nil {
				return criA.Compare(criB)
			}
		}
	}
	return strings.Compare(a, b)
}

func containsVolumeType(options *ske.ProviderOptions, volumeType string) bool {
	for _, item := range options.GetVolumeTypes() {
		if item.GetName() == volumeType {
			return true
		}
	}
	return false
}
