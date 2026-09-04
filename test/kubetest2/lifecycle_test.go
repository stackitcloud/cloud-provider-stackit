package kubetest2

import (
	"net/http"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	oapierror "github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	authorization "github.com/stackitcloud/stackit-sdk-go/services/authorization/v2api"
	resourcemanager "github.com/stackitcloud/stackit-sdk-go/services/resourcemanager/v0api"
	serviceaccount "github.com/stackitcloud/stackit-sdk-go/services/serviceaccount/v2api"
	serviceenablement "github.com/stackitcloud/stackit-sdk-go/services/serviceenablement/v2api"
	"github.com/stackitcloud/stackit-sdk-go/services/ske"
)

var _ = Describe("Up", func() {
	It("uses discovered project and writes kubeconfig", func() {
		d := newTestDeployer()
		configureValidUpInputs(d)

		cachedKey := `{"credentials":{"privateKey":"cached"}}`
		Expect(os.WriteFile(d.serviceAccountKeyPath, []byte(cachedKey), 0o600)).To(Succeed())

		d.projectClient = &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{
				*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
			},
		}
		serviceAccountEmail := d.serviceAccountName() + "@sa.stackit.cloud"
		d.serviceAccountClient = &fakeServiceAccountClient{
			listResult: []serviceaccount.ServiceAccount{
				*serviceAccountFixture(serviceAccountEmail, "project-123"),
			},
		}
		d.authorizationClient = &fakeAuthorizationClient{
			listMembersResult: []authorization.Member{
				*authorization.NewMember(childProjectSKERole, serviceAccountEmail),
				*authorization.NewMember(childProjectStorageRole, serviceAccountEmail),
			},
		}
		d.serviceEnablementClient = &fakeServiceEnablementClient{getStatusResult: serviceenablement.NewServiceStatus()}

		fakeSKE := &fakeSKEClient{
			providerOptions:      providerOptionsFixture(),
			createOrUpdateResult: ske.NewClusterWithDefaults(),
			waitReadyResult:      ske.NewClusterWithDefaults(),
			kubeconfigResult: func() *ske.Kubeconfig {
				cfg := ske.NewKubeconfig()
				cfg.SetKubeconfig("apiVersion: v1\n")
				return cfg
			}(),
		}
		d.skeClientFactory = func(_, serviceAccount, _ string) (skeClient, error) {
			Expect(serviceAccount).To(Equal(cachedKey))
			return fakeSKE, nil
		}

		Expect(d.Up()).To(Succeed())
		Expect(fakeSKE.lastCreateProjectID).To(Equal("project-123"))
		Expect(fakeSKE.lastCreateClusterName).To(Equal(d.clusterName()))

		kubeconfigBytes, err := os.ReadFile(d.kubeconfigPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(kubeconfigBytes)).To(Equal("apiVersion: v1\n"))
	})
})

var _ = Describe("Down", func() {
	It("succeeds when project is missing", func() {
		d := newTestDeployer()
		projectClient := &fakeProjectClient{}
		d.projectClient = projectClient

		Expect(d.Down()).To(Succeed())
		Expect(projectClient.deleteCalls).To(Equal(0))
	})

	It("deletes project", func() {
		d := newTestDeployer()
		d.projectClient = &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{
				*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
			},
		}

		Expect(d.Down()).To(Succeed())
		projectClient := d.projectClient.(*fakeProjectClient)
		Expect(projectClient.deleteCalls).To(Equal(1))
		Expect(projectClient.waitDeletedCalls).To(Equal(1))
	})
})

var _ = Describe("IsUp", func() {
	It("returns false when no project", func() {
		d := newTestDeployer()
		d.projectClient = &fakeProjectClient{}

		isUp, err := d.IsUp()
		Expect(err).NotTo(HaveOccurred())
		Expect(isUp).To(BeFalse())
	})

	It("returns error when project without cached key", func() {
		d := newTestDeployer()
		d.projectClient = &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{
				*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
			},
		}

		_, err := d.IsUp()
		Expect(err).To(MatchError(ContainSubstring("child service-account key cache")))
	})

	It("queries cluster when project with cached key", func() {
		d := newTestDeployer()
		d.projectClient = &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{
				*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
			},
		}
		cachedKey := `{"credentials":{"privateKey":"cached"}}`
		Expect(os.WriteFile(d.serviceAccountKeyPath, []byte(cachedKey), 0o600)).To(Succeed())

		fakeSKE := &fakeSKEClient{
			getClusterResult: healthyClusterFixture(),
		}
		d.skeClientFactory = func(_, serviceAccount, _ string) (skeClient, error) {
			Expect(serviceAccount).To(Equal(cachedKey))
			return fakeSKE, nil
		}

		isUp, err := d.IsUp()
		Expect(err).NotTo(HaveOccurred())
		Expect(isUp).To(BeTrue())
		Expect(fakeSKE.lastGetProjectID).To(Equal("project-123"))
	})

	It("returns false when cluster missing", func() {
		d := newTestDeployer()
		d.projectClient = &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{
				*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
			},
		}
		cachedKey := `{"credentials":{"privateKey":"cached"}}`
		Expect(os.WriteFile(d.serviceAccountKeyPath, []byte(cachedKey), 0o600)).To(Succeed())

		fakeSKE := &fakeSKEClient{
			getClusterErr: &oapierror.GenericOpenAPIError{StatusCode: http.StatusNotFound},
		}
		d.skeClientFactory = func(_, _, _ string) (skeClient, error) {
			return fakeSKE, nil
		}

		isUp, err := d.IsUp()
		Expect(err).NotTo(HaveOccurred())
		Expect(isUp).To(BeFalse())
	})
})
