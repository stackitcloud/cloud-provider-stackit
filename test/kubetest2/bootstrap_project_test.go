package kubetest2

import (
	"context"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	oapierror "github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	resourcemanager "github.com/stackitcloud/stackit-sdk-go/services/resourcemanager/v0api"
	serviceenablement "github.com/stackitcloud/stackit-sdk-go/services/serviceenablement/v2api"
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

var _ = Describe("resolveManagedProject", func() {
	It("creates a project when missing", func() {
		d := newTestDeployer()
		projectClient := &fakeProjectClient{
			createProjectResult: projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
			waitActiveResult:    projectResponseFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
		}
		d.projectClient = projectClient

		project, err := d.resolveManagedProject(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(project.ProjectID).To(Equal("project-123"))
		Expect(projectClient.createCalls).To(Equal(1))
		Expect(projectClient.waitActiveCalls).To(Equal(1))
		Expect(projectClient.lastCreateParentContainerID).To(Equal(d.parentContainerID))
		Expect(projectClient.lastCreateName).To(Equal(d.projectName()))
		Expect(projectClient.lastCreateOwnerEmail).To(Equal(d.projectMemberEmail))
		Expect(projectClient.lastCreateLabels).To(Equal(d.managedProjectLabels()))
	})

	It("errors on multiple matches", func() {
		d := newTestDeployer()
		d.projectClient = &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{
				*projectFixture(d.projectName(), "project-1", "container-1", d.managedProjectLabels()),
				*projectFixture(d.projectName(), "project-2", "container-2", d.managedProjectLabels()),
			},
		}

		_, err := d.findManagedProject(context.Background())
		Expect(err).To(MatchError(ContainSubstring("found 2 managed STACKIT projects")))
	})
})

var _ = Describe("ensureProject", func() {
	It("resolves the managed project and enables the SKE service", func() {
		d := newTestDeployer()
		d.projectClient = &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{
				*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
			},
		}
		serviceEnablementClient := &fakeServiceEnablementClient{getStatusResult: serviceenablement.NewServiceStatus()}
		d.serviceEnablementClient = serviceEnablementClient

		Expect(d.ensureProject(context.Background())).To(Succeed())
		Expect(d.projectID).To(Equal("project-123"))
		Expect(serviceEnablementClient.lastGetStatusProjectID).To(Equal("project-123"))
		Expect(serviceEnablementClient.enableCalls).To(Equal(0))
	})
})

var _ = Describe("ensureSKEServiceEnabled", func() {
	It("skips when already enabled", func() {
		d := newTestDeployer()
		client := &fakeServiceEnablementClient{getStatusResult: serviceenablement.NewServiceStatus()}
		d.serviceEnablementClient = client

		Expect(d.ensureSKEServiceEnabled(context.Background(), "project-123")).To(Succeed())
		Expect(client.enableCalls).To(Equal(0))
		Expect(client.waitCalls).To(Equal(0))
		Expect(client.lastGetStatusServiceID).To(Equal(skeServiceID))
	})

	It("enables when not found", func() {
		d := newTestDeployer()
		client := &fakeServiceEnablementClient{
			getStatusErr: &oapierror.GenericOpenAPIError{StatusCode: http.StatusNotFound},
		}
		d.serviceEnablementClient = client

		Expect(d.ensureSKEServiceEnabled(context.Background(), "project-123")).To(Succeed())
		Expect(client.enableCalls).To(Equal(1))
		Expect(client.waitCalls).To(Equal(1))
		Expect(client.lastEnableProjectID).To(Equal("project-123"))
		Expect(client.lastEnableServiceID).To(Equal(skeServiceID))
	})

	It("enables when disabled", func() {
		d := newTestDeployer()
		client := &fakeServiceEnablementClient{
			getStatusResult: disabledServiceStatusFixture(),
		}
		d.serviceEnablementClient = client

		Expect(d.ensureSKEServiceEnabled(context.Background(), "project-123")).To(Succeed())
		Expect(client.enableCalls).To(Equal(1))
		Expect(client.waitCalls).To(Equal(1))
	})

	It("fails on get status error", func() {
		d := newTestDeployer()
		client := &fakeServiceEnablementClient{
			getStatusErr: &oapierror.GenericOpenAPIError{StatusCode: http.StatusForbidden},
		}
		d.serviceEnablementClient = client

		err := d.ensureSKEServiceEnabled(context.Background(), "project-123")
		Expect(err).To(HaveOccurred())
		Expect(client.enableCalls).To(Equal(0))
	})
})
