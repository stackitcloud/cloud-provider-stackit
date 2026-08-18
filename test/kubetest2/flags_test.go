package kubetest2

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func newValidTestDeployer() *Deployer {
	d := newTestDeployer()
	configureValidUpInputs(d)
	d.nodeCount = defaultNodeCount
	d.volumeSizeGiB = defaultVolumeSizeGiB
	d.kubeconfigExpiresIn = defaultKubeconfigExpiration
	return d
}

var _ = Describe("validate", func() {
	It("accepts a valid configuration", func() {
		Expect(newValidTestDeployer().validate()).To(Succeed())
	})

	DescribeTable("rejects invalid configuration",
		func(mutate func(*Deployer), wantErrContains string) {
			d := newValidTestDeployer()
			mutate(d)
			Expect(d.validate()).To(MatchError(ContainSubstring(wantErrContains)))
		},
		Entry("missing region", func(d *Deployer) { d.region = "" }, "--region is required"),
		Entry("missing kubernetes version", func(d *Deployer) { d.kubernetesVersion = "" }, "--kubernetes-version is required"),
		Entry("missing availability zone", func(d *Deployer) { d.availabilityZone = "" }, "--availability-zone is required"),
		Entry("missing machine type", func(d *Deployer) { d.machineType = "" }, "--machine-type is required"),
		Entry("missing node image name", func(d *Deployer) { d.nodeImageName = "" }, "--node-image-name is required"),
		Entry("missing node image version", func(d *Deployer) { d.nodeImageVersion = "" }, "--node-image-version is required"),
		Entry("empty nodepool name", func(d *Deployer) { d.nodepoolName = "" }, "--nodepool-name must not be empty"),
		Entry("nodepool name too long", func(d *Deployer) { d.nodepoolName = "this-name-is-way-too-long" }, "--nodepool-name must be 15 characters or fewer"),
		Entry("zero node count", func(d *Deployer) { d.nodeCount = 0 }, "--node-count must be greater than 0"),
		Entry("zero volume size", func(d *Deployer) { d.volumeSizeGiB = 0 }, "--volume-size must be greater than 0"),
		Entry("kubeconfig expiration too small", func(d *Deployer) { d.kubeconfigExpiresIn = minKubeconfigExpiration - 1 }, "--kubeconfig-expiration-seconds must be between"),
		Entry("kubeconfig expiration too large", func(d *Deployer) { d.kubeconfigExpiresIn = maxKubeconfigExpiration + 1 }, "--kubeconfig-expiration-seconds must be between"),
		Entry("empty run id", func(d *Deployer) { d.options = fakeOptions{runID: "", runDir: d.options.RunDir()} }, "run-id must not be empty"),
	)
})
