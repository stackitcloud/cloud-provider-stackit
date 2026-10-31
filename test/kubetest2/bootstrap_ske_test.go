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

	It("resolves minor kubernetes version to latest supported patch version", func() {
		d := newValidTestDeployer()
		d.kubernetesVersion = "1.36"
		d.nodeImageName = "flatcar"
		d.nodeImageVersion = ""
		d.skeClient = &fakeSKEClient{providerOptions: multiVersionProviderOptionsFixture()}

		Expect(d.validateProviderOptions(context.Background())).To(Succeed())
		Expect(d.kubernetesVersion).To(Equal("1.36.5"))
		Expect(d.nodeImageVersion).To(Equal("4593.2.5-containerd2.1.9"))
	})

	It("resolves latest flatcar version preferring containerd suffix and higher containerd version", func() {
		d := newValidTestDeployer()
		d.kubernetesVersion = "1.36"
		d.nodeImageName = "flatcar"
		d.nodeImageVersion = ""
		opts := multiVersionProviderOptionsFixture()
		images := opts.GetMachineImages()
		for i := range images {
			if images[i].GetName() == "flatcar" {
				images[i].SetVersions([]ske.MachineImageVersion{
					newMachineImageVersionFixture("4593.2.5", "supported"),
					newMachineImageVersionFixture("4593.2.5-containerd2.1.9", "supported"),
					newMachineImageVersionFixture("4593.2.5-containerd2.2.0", "supported"),
				})
			}
		}
		d.skeClient = &fakeSKEClient{providerOptions: opts}

		Expect(d.validateProviderOptions(context.Background())).To(Succeed())
		Expect(d.nodeImageVersion).To(Equal("4593.2.5-containerd2.2.0"))
	})

	DescribeTable("resolves ubuntu versions",
		func(requestedVersion, versionState, wantVersion string) {
			d := newValidTestDeployer()
			d.kubernetesVersion = "1.36"
			d.nodeImageName = "ubuntu"
			d.nodeImageVersion = requestedVersion
			d.versionState = versionState
			d.skeClient = &fakeSKEClient{providerOptions: multiVersionProviderOptionsFixture()}

			Expect(d.validateProviderOptions(context.Background())).To(Succeed())
			Expect(d.nodeImageVersion).To(Equal(wantVersion))
		},
		Entry("omitted version picks latest supported across all majors", "", "supported", "2604.20260921.0"),
		Entry("major 22 picks latest supported 2204", "22", "supported", "2204.20260826.0"),
		Entry("major 24 picks latest supported 2404", "24", "supported", "2404.20260901.0"),
		Entry("major 26 picks latest supported 2604", "26", "supported", "2604.20260921.0"),
		Entry("major 22 with preview state picks latest preview 2204", "22", "preview", "2204.20261005.0"),
		Entry("exact version is preserved when state matches", "2204.20260803.0", "supported", "2204.20260803.0"),
	)

	It("resolves preview kubernetes and flatcar versions when versionState is preview", func() {
		d := newValidTestDeployer()
		d.kubernetesVersion = "1.36"
		d.nodeImageName = "flatcar"
		d.nodeImageVersion = ""
		d.versionState = "preview"
		d.skeClient = &fakeSKEClient{providerOptions: multiVersionProviderOptionsFixture()}

		Expect(d.validateProviderOptions(context.Background())).To(Succeed())
		Expect(d.kubernetesVersion).To(Equal("1.36.6"))
		Expect(d.nodeImageVersion).To(Equal("4757.2.1-containerd2.2.9"))
	})

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
		Entry("unsupported ubuntu major version", func(d *Deployer) { d.nodeImageName = "ubuntu"; d.nodeImageVersion = "20" }, "unsupported node image"),
		Entry("mismatched kubernetes version state", func(d *Deployer) { d.versionState = "preview" }, "unsupported --kubernetes-version"),
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
