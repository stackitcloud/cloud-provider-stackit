package kubetest2

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("matchesManagedProject", func() {
	It("matches a project with the expected name and labels", func() {
		d := newTestDeployer()
		project := projectFixture(d.projectName(), "project-1", "container-1", d.managedProjectLabels())
		Expect(d.matchesManagedProject(project)).To(BeTrue())
	})

	It("rejects a project with a different name", func() {
		d := newTestDeployer()
		project := projectFixture("other-name", "project-1", "container-1", d.managedProjectLabels())
		Expect(d.matchesManagedProject(project)).To(BeFalse())
	})

	It("rejects a project without labels", func() {
		d := newTestDeployer()
		project := projectFixture(d.projectName(), "project-1", "container-1", nil)
		Expect(d.matchesManagedProject(project)).To(BeFalse())
	})

	DescribeTable("rejects a project with mismatched labels",
		func(labels map[string]string) {
			d := newTestDeployer()
			project := projectFixture(d.projectName(), "project-1", "container-1", labels)
			Expect(d.matchesManagedProject(project)).To(BeFalse())
		},
		Entry("wrong scope", map[string]string{
			projectLabelScopeKey:   "PRIVATE",
			projectLabelManagedKey: projectLabelManagedValue,
			projectLabelRunIDKey:   runTokenForRun("run-123"),
		}),
		Entry("missing managed label", map[string]string{
			projectLabelScopeKey: projectLabelScopeValue,
			projectLabelRunIDKey: runTokenForRun("run-123"),
		}),
		Entry("wrong run id", map[string]string{
			projectLabelScopeKey:   projectLabelScopeValue,
			projectLabelManagedKey: projectLabelManagedValue,
			projectLabelRunIDKey:   "deadbeef",
		}),
	)
})

var _ = Describe("matchesManagedServiceAccountEmail", func() {
	It("matches the managed service account prefix", func() {
		d := newTestDeployer()
		Expect(d.matchesManagedServiceAccountEmail(d.serviceAccountName() + "@sa.stackit.cloud")).To(BeTrue())
	})

	DescribeTable("rejects non-matching emails",
		func(email string) {
			d := newTestDeployer()
			Expect(d.matchesManagedServiceAccountEmail(email)).To(BeFalse())
		},
		Entry("empty email", ""),
		Entry("missing at sign", "kt2-no-separator"),
		Entry("different local prefix", "other-account@sa.stackit.cloud"),
	)
})
