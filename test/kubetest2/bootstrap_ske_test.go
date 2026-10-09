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

	DescribeTable("rejects unsupported options",
		func(mutate func(*Deployer), wantErrContains string) {
			d := newValidTestDeployer()
			d.skeClient = &fakeSKEClient{providerOptions: providerOptionsFixture()}
			mutate(d)
			Expect(d.validateProviderOptions(context.Background())).To(MatchError(ContainSubstring(wantErrContains)))
		},
		Entry("unsupported kubernetes version", func(d *Deployer) { d.kubernetesVersion = "9.9.9" }, "unsupported --kubernetes-version"),
		Entry("unsupported availability zone", func(d *Deployer) { d.availabilityZone = "eu99-9" }, "unsupported --availability-zone"),
		Entry("unsupported machine type", func(d *Deployer) { d.machineType = "x9.9" }, "unsupported --machine-type"),
		Entry("unsupported node image name", func(d *Deployer) { d.nodeImageName = "fedora" }, "unsupported node image"),
		Entry("unsupported node image version", func(d *Deployer) { d.nodeImageVersion = "v99" }, "unsupported node image"),
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

		Expect(nodepool.GetMinimum()).To(Equal(int32(d.nodeCount)))
		Expect(nodepool.GetMaximum()).To(Equal(int32(d.nodeCount)))

		volume := nodepool.GetVolume()
		Expect(volume.GetSize()).To(Equal(int32(d.volumeSizeGiB)))
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
