package kubetest2

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ske "github.com/stackitcloud/stackit-sdk-go/services/ske/v2api"
)

var _ = Describe("validateProviderOptions", func() {
	It("accepts supported options", func() {
		d := newValidTestDeployer()
		d.skeClient = &fakeSKEClient{providerOptions: providerOptionsFixture()}
		Expect(d.validateProviderOptions(context.Background())).To(Succeed())
	})

	DescribeTable("resolves kubernetes versions",
		func(kubernetesVersion string, useLatestKubernetesVersion bool, kubernetesVersionState, wantVersion string) {
			d := newValidTestDeployer()
			d.kubernetesVersion = kubernetesVersion
			d.useLatestKubernetesVersion = useLatestKubernetesVersion
			d.nodeImageName = "flatcar"
			d.nodeImageVersion = ""
			d.kubernetesVersionState = kubernetesVersionState
			opts := providerOptionsFixture()
			flatcar := ske.NewMachineImage()
			flatcar.SetName("flatcar")
			flatcar.SetVersions([]ske.MachineImageVersion{
				newMachineImageVersionFixture("4593.2.2", versionStateSupported),
			})
			opts.SetKubernetesVersions([]ske.KubernetesVersion{
				newKubernetesVersionFixture("1.35.7", versionStateSupported),
				newKubernetesVersionFixture("1.36.3", versionStateDeprecated),
				newKubernetesVersionFixture("1.36.4", versionStateSupported),
				newKubernetesVersionFixture("1.36.5", versionStateSupported),
				newKubernetesVersionFixture("1.36.6", versionStatePreview),
			})
			opts.SetMachineImages([]ske.MachineImage{*flatcar})
			d.skeClient = &fakeSKEClient{providerOptions: opts}

			Expect(d.validateProviderOptions(context.Background())).To(Succeed())
			Expect(d.kubernetesVersion).To(Equal(wantVersion))
			Expect(d.nodeImageVersion).To(Equal("4593.2.2"))
		},
		Entry("resolves minor kubernetes version to latest supported patch version", "1.36", false, versionStateSupported, "1.36.5"),
		Entry("resolves minor preview kubernetes version to latest supported patch version", "1.36", false, versionStatePreview, "1.36.6"),
		Entry("exact version is used", "1.36.4", false, versionStateSupported, "1.36.4"),
		Entry("uses latest supported kubernetes version when flag is set", "", true, versionStateSupported, "1.36.5"),
		Entry("uses latest preview kubernetes version when flag is set", "", true, versionStatePreview, "1.36.6"),
		Entry("use-latest-kubernetes-version overrules explicit kubernetes-version", "1.35.7", true, versionStateSupported, "1.36.5"),
	)

	DescribeTable("resolves machine image versions",
		func(imageName, imageVersion, imageVersionState, wantVersion string) {
			d := newValidTestDeployer()
			d.kubernetesVersion = "1.36"
			d.nodeImageName = imageName
			d.nodeImageVersion = imageVersion
			d.nodeImageVersionState = imageVersionState

			opts := providerOptionsFixture()
			opts.SetKubernetesVersions([]ske.KubernetesVersion{
				newKubernetesVersionFixture("1.36.5", versionStateSupported),
			})

			flatcar := ske.NewMachineImage()
			flatcar.SetName("flatcar")
			flatcar.SetVersions([]ske.MachineImageVersion{
				newMachineImageVersionFixture("4593.2.2", versionStateDeprecated),
				newMachineImageVersionFixture("4593.2.4-containerd2.1.9", versionStateSupported),
				newMachineImageVersionFixture("4593.2.5-containerd2.1.9", versionStateSupported),
				newMachineImageVersionFixture("4757.2.1-containerd2.2.9", versionStatePreview),
				newMachineImageVersionFixture("4757.2.1-containerd2.2.10", versionStatePreview),
			})

			ubuntu := ske.NewMachineImage()
			ubuntu.SetName("ubuntu")
			ubuntu.SetVersions([]ske.MachineImageVersion{
				newMachineImageVersionFixture("2204.20260629.0", versionStateDeprecated),
				newMachineImageVersionFixture("2204.20260803.0", versionStateSupported),
				newMachineImageVersionFixture("2204.20260826.0", versionStateSupported),
				newMachineImageVersionFixture("2204.20261005.0", versionStatePreview),
				newMachineImageVersionFixture("2404.20260901.0", versionStateSupported),
				newMachineImageVersionFixture("2604.20260921.0", versionStateSupported),
			})

			opts.SetMachineImages([]ske.MachineImage{*flatcar, *ubuntu})

			d.skeClient = &fakeSKEClient{providerOptions: opts}

			Expect(d.validateProviderOptions(context.Background())).To(Succeed())
			Expect(d.nodeImageVersion).To(Equal(wantVersion))
		},
		Entry("omitted version picks latest supported across all majors", "ubuntu", "", versionStateSupported, "2604.20260921.0"),
		Entry("major 2204 picks latest supported 2204", "ubuntu", "2204", versionStateSupported, "2204.20260826.0"),
		Entry("major 2404 picks latest supported 2404", "ubuntu", "2404", versionStateSupported, "2404.20260901.0"),
		Entry("major 2604 picks latest supported 2604", "ubuntu", "2604", versionStateSupported, "2604.20260921.0"),
		Entry("major 2204 with preview state picks latest preview 2204", "ubuntu", "2204", versionStatePreview, "2204.20261005.0"),
		Entry("exact version is preserved when state matches", "ubuntu", "2204.20260803.0", versionStateSupported, "2204.20260803.0"),
		Entry("flatcar major 4593 picks latest supported 4593", "flatcar", "4593", versionStateSupported, "4593.2.5-containerd2.1.9"),
		Entry("flatcar with latest contained version in preview", "flatcar", "", versionStatePreview, "4757.2.1-containerd2.2.10"),
	)

	DescribeTable("rejects unsupported options",
		func(mutate func(*Deployer), wantErrContains string) {
			d := newValidTestDeployer()
			d.skeClient = &fakeSKEClient{providerOptions: providerOptionsFixture()}
			mutate(d)
			Expect(d.validateProviderOptions(context.Background())).To(MatchError(ContainSubstring(wantErrContains)))
		},
		Entry("unsupported kubernetes version", func(d *Deployer) { d.kubernetesVersion = "9.9.9" }, "unsupported --kubernetes-version"),
		Entry("unsupported kubernetes minor version", func(d *Deployer) { d.kubernetesVersion = "1.99" }, "unsupported --kubernetes-version"),
		Entry("unsupported availability zone", func(d *Deployer) { d.availabilityZone = "eu99-9" }, "unsupported --availability-zone"),
		Entry("unsupported machine type", func(d *Deployer) { d.machineType = "x9.9" }, "unsupported --machine-type"),
		Entry("unsupported node image name", func(d *Deployer) { d.nodeImageName = "fedora" }, "unsupported node image"),
		Entry("unsupported node image version", func(d *Deployer) { d.nodeImageVersion = "v99" }, "unsupported node image"),
		Entry("unsupported ubuntu major version", func(d *Deployer) { d.nodeImageName = "ubuntu"; d.nodeImageVersion = "2004" }, "unsupported node image"),
		Entry("mismatched kubernetes version state", func(d *Deployer) { d.kubernetesVersionState = versionStatePreview }, "unsupported --kubernetes-version"),
		Entry("mismatched latest kubernetes version state", func(d *Deployer) {
			d.useLatestKubernetesVersion = true
			d.kubernetesVersionState = versionStatePreview
		}, "unsupported --kubernetes-version"),
		Entry("mismatched node image version state", func(d *Deployer) { d.nodeImageVersionState = versionStatePreview }, "unsupported node image"),
		Entry("unsupported volume type", func(d *Deployer) { d.volumeType = "nvme" }, "unsupported --volume-type"),
	)

	It("fails when listing provider options errors", func() {
		d := newValidTestDeployer()
		d.skeClient = &fakeSKEClient{providerOptionsErr: errors.New("boom")}
		Expect(d.validateProviderOptions(context.Background())).To(MatchError(ContainSubstring("list SKE provider options")))
	})

	It("allows an empty volume type", func() {
		d := newValidTestDeployer()
		d.volumeType = ""
		d.skeClient = &fakeSKEClient{providerOptions: providerOptionsFixture()}
		Expect(d.validateProviderOptions(context.Background())).To(Succeed())
	})

	It("fails when provider options response is nil", func() {
		d := newValidTestDeployer()
		d.skeClient = &fakeSKEClient{providerOptions: nil}
		Expect(d.validateProviderOptions(context.Background())).To(MatchError(ContainSubstring("returned nil response")))
	})
})

var _ = Describe("clusterPayload", func() {
	It("builds the expected payload", func() {
		d := newValidTestDeployer()
		payload := d.clusterPayload()

		kubernetes := payload.GetKubernetes()
		Expect(kubernetes.GetVersion()).To(Equal("1.31.0"))

		nodepools := payload.GetNodepools()
		Expect(nodepools).To(HaveLen(1))

		nodepool := nodepools[0]
		Expect(nodepool.GetName()).To(Equal(defaultNodepoolName))
		Expect(nodepool.GetAvailabilityZones()).To(ConsistOf(defaultAvailabilityZone))

		machine := nodepool.GetMachine()
		Expect(machine.GetType()).To(Equal("g1.2"))
		image := machine.GetImage()
		Expect(image.GetName()).To(Equal("ubuntu"))
		Expect(image.GetVersion()).To(Equal("v1"))

		Expect(nodepool.GetMinimum()).To(Equal(d.nodeCount))
		Expect(nodepool.GetMaximum()).To(Equal(d.nodeCount))

		volume := nodepool.GetVolume()
		Expect(volume.GetSize()).To(Equal(d.volumeSizeGiB))
		Expect(volume.GetType()).To(Equal("storage"))

		Expect(nodepool.GetAllowSystemComponents()).To(BeTrue())
	})

	It("omits the volume type when unset", func() {
		d := newValidTestDeployer()
		d.volumeType = ""
		payload := d.clusterPayload()

		Expect(payload.GetNodepools()).To(HaveLen(1))
		volume := payload.GetNodepools()[0].GetVolume()
		Expect(volume.GetType()).To(BeEmpty())
	})
})

var _ = Describe("writeKubeconfig", func() {
	It("rejects empty kubeconfig content", func() {
		d := newValidTestDeployer()
		d.skeClient = &fakeSKEClient{
			kubeconfigResult: ske.NewKubeconfig(),
		}
		Expect(d.writeKubeconfig(context.Background(), "test-cluster")).To(MatchError(ContainSubstring("returned empty kubeconfig")))
	})
})
