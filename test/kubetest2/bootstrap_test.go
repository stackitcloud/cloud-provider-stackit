package kubetest2

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	oapierror "github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	authorization "github.com/stackitcloud/stackit-sdk-go/services/authorization/v2api"
	resourcemanager "github.com/stackitcloud/stackit-sdk-go/services/resourcemanager/v0api"
	serviceaccount "github.com/stackitcloud/stackit-sdk-go/services/serviceaccount/v2api"
	serviceenablement "github.com/stackitcloud/stackit-sdk-go/services/serviceenablement/v2api"
	"github.com/stackitcloud/stackit-sdk-go/services/ske"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/kubetest2/pkg/types"
)

const validServiceAccountKey = `{"credentials":{"iss":"owner@example.com"}}`

type fakeOptions struct {
	runID  string
	runDir string
}

func (o fakeOptions) HelpRequested() bool       { return false }
func (o fakeOptions) ShouldBuild() bool         { return false }
func (o fakeOptions) ShouldUp() bool            { return false }
func (o fakeOptions) ShouldDown() bool          { return false }
func (o fakeOptions) ShouldTest() bool          { return false }
func (o fakeOptions) SkipTestJUnitReport() bool { return false }
func (o fakeOptions) RunID() string             { return o.runID }
func (o fakeOptions) RunDir() string            { return o.runDir }
func (o fakeOptions) RundirInArtifacts() bool   { return false }
func (o fakeOptions) PostTestCmd() []string     { return nil }
func (o fakeOptions) PreTestCmd() []string      { return nil }

type fakeProjectClient struct {
	listProjectsResult []resourcemanager.Project
	listProjectsErr    error

	createProjectResult *resourcemanager.Project
	createProjectErr    error
	waitActiveResult    *resourcemanager.GetProjectResponse
	waitActiveErr       error

	deleteErr      error
	waitDeletedErr error

	createCalls      int
	waitActiveCalls  int
	deleteCalls      int
	waitDeletedCalls int

	lastListParentContainerID   string
	lastCreateParentContainerID string
	lastCreateName              string
	lastCreateOwnerEmail        string
	lastCreateLabels            map[string]string
	lastWaitActiveContainerID   string
	lastDeletedProjectID        string
	lastWaitDeletedProjectID    string
}

func (c *fakeProjectClient) ListProjects(_ context.Context, parentContainerID string) ([]resourcemanager.Project, error) {
	c.lastListParentContainerID = parentContainerID
	return c.listProjectsResult, c.listProjectsErr
}

func (c *fakeProjectClient) CreateProject(_ context.Context, parentContainerID, name, ownerEmail string, labels map[string]string) (*resourcemanager.Project, error) {
	c.createCalls++
	c.lastCreateParentContainerID = parentContainerID
	c.lastCreateName = name
	c.lastCreateOwnerEmail = ownerEmail
	c.lastCreateLabels = labels
	return c.createProjectResult, c.createProjectErr
}

func (c *fakeProjectClient) WaitForProjectActive(_ context.Context, containerID string) (*resourcemanager.GetProjectResponse, error) {
	c.waitActiveCalls++
	c.lastWaitActiveContainerID = containerID
	return c.waitActiveResult, c.waitActiveErr
}

func (c *fakeProjectClient) DeleteProject(_ context.Context, projectID string) error {
	c.deleteCalls++
	c.lastDeletedProjectID = projectID
	return c.deleteErr
}

func (c *fakeProjectClient) WaitForProjectDeleted(_ context.Context, projectID string) error {
	c.waitDeletedCalls++
	c.lastWaitDeletedProjectID = projectID
	return c.waitDeletedErr
}

type fakeServiceAccountClient struct {
	listResult []serviceaccount.ServiceAccount
	listErr    error

	createResult *serviceaccount.ServiceAccount
	createErr    error

	createKeyResult *serviceaccount.CreateServiceAccountKeyResponse
	createKeyErr    error

	createCalls    int
	createKeyCalls int

	lastProjectIDForList      string
	lastProjectIDForCreate    string
	lastCreatedName           string
	lastProjectIDForCreateKey string
	lastCreateKeyEmail        string
}

func (c *fakeServiceAccountClient) ListServiceAccounts(_ context.Context, projectID string) ([]serviceaccount.ServiceAccount, error) {
	c.lastProjectIDForList = projectID
	return c.listResult, c.listErr
}

func (c *fakeServiceAccountClient) CreateServiceAccount(_ context.Context, projectID, name string) (*serviceaccount.ServiceAccount, error) {
	c.createCalls++
	c.lastProjectIDForCreate = projectID
	c.lastCreatedName = name
	return c.createResult, c.createErr
}

func (c *fakeServiceAccountClient) CreateServiceAccountKey(_ context.Context, projectID, serviceAccountEmail string) (*serviceaccount.CreateServiceAccountKeyResponse, error) {
	c.createKeyCalls++
	c.lastProjectIDForCreateKey = projectID
	c.lastCreateKeyEmail = serviceAccountEmail
	return c.createKeyResult, c.createKeyErr
}

type fakeAuthorizationClient struct {
	listMembersResult []authorization.Member
	listMembersErr    error
	addMembersErr     error

	addCalls int

	lastResourceType string
	lastResourceID   string
	lastAddedType    string
	lastAddedID      string
	lastAddedMembers []authorization.Member
}

func (c *fakeAuthorizationClient) ListMembers(_ context.Context, resourceType, resourceID string) ([]authorization.Member, error) {
	c.lastResourceType = resourceType
	c.lastResourceID = resourceID
	return c.listMembersResult, c.listMembersErr
}

func (c *fakeAuthorizationClient) AddMembers(_ context.Context, resourceID, resourceType string, members []authorization.Member) error {
	c.addCalls++
	c.lastAddedID = resourceID
	c.lastAddedType = resourceType
	c.lastAddedMembers = members
	return c.addMembersErr
}

type fakeServiceEnablementClient struct {
	getStatusResult *serviceenablement.ServiceStatus
	getStatusErr    error
	enableErr       error
	waitErr         error

	enableCalls int
	waitCalls   int

	lastGetStatusRegion    string
	lastGetStatusProjectID string
	lastGetStatusServiceID string
	lastEnableRegion       string
	lastEnableProjectID    string
	lastEnableServiceID    string
	lastWaitRegion         string
	lastWaitProjectID      string
	lastWaitServiceID      string
}

func (c *fakeServiceEnablementClient) GetServiceStatus(_ context.Context, region, projectID, serviceID string) (*serviceenablement.ServiceStatus, error) {
	c.lastGetStatusRegion = region
	c.lastGetStatusProjectID = projectID
	c.lastGetStatusServiceID = serviceID
	return c.getStatusResult, c.getStatusErr
}

func (c *fakeServiceEnablementClient) EnableService(_ context.Context, region, projectID, serviceID string) error {
	c.enableCalls++
	c.lastEnableRegion = region
	c.lastEnableProjectID = projectID
	c.lastEnableServiceID = serviceID
	return c.enableErr
}

func (c *fakeServiceEnablementClient) WaitForServiceEnabled(_ context.Context, region, projectID, serviceID string) error {
	c.waitCalls++
	c.lastWaitRegion = region
	c.lastWaitProjectID = projectID
	c.lastWaitServiceID = serviceID
	return c.waitErr
}

type fakeSKEClient struct {
	providerOptions    *ske.ProviderOptions
	providerOptionsErr error
	getClusterResult   *ske.Cluster
	getClusterErr      error

	createOrUpdateResult *ske.Cluster
	createOrUpdateErr    error
	waitReadyResult      *ske.Cluster
	waitReadyErr         error
	kubeconfigResult     *ske.Kubeconfig
	kubeconfigErr        error

	deleteClusterCalled bool
	waitDeletedCalled   bool

	lastGetProjectID        string
	lastCreateProjectID     string
	lastCreateRegion        string
	lastCreateClusterName   string
	lastKubeconfigProjectID string
	lastExpirationSeconds   int64
}

func (c *fakeSKEClient) GetCluster(_ context.Context, projectID, _, _ string) (*ske.Cluster, error) {
	c.lastGetProjectID = projectID
	return c.getClusterResult, c.getClusterErr
}

func (c *fakeSKEClient) ListProviderOptions(_ context.Context, _ string) (*ske.ProviderOptions, error) {
	return c.providerOptions, c.providerOptionsErr
}

func (c *fakeSKEClient) CreateOrUpdateCluster(_ context.Context, projectID, region, name string, _ ske.CreateOrUpdateClusterPayload) (*ske.Cluster, error) {
	c.lastCreateProjectID = projectID
	c.lastCreateRegion = region
	c.lastCreateClusterName = name
	return c.createOrUpdateResult, c.createOrUpdateErr
}

func (c *fakeSKEClient) WaitForClusterReady(_ context.Context, _, _, _ string) (*ske.Cluster, error) {
	return c.waitReadyResult, c.waitReadyErr
}

func (c *fakeSKEClient) CreateKubeconfig(_ context.Context, projectID, _, _ string, expirationSeconds int64) (*ske.Kubeconfig, error) {
	c.lastKubeconfigProjectID = projectID
	c.lastExpirationSeconds = expirationSeconds
	return c.kubeconfigResult, c.kubeconfigErr
}

func (c *fakeSKEClient) DeleteCluster(_ context.Context, _, _, _ string) error {
	c.deleteClusterCalled = true
	return nil
}

func (c *fakeSKEClient) WaitForClusterDeleted(_ context.Context, _, _, _ string) error {
	c.waitDeletedCalled = true
	return nil
}

var _ = Describe("loadEnvironment", func() {
	envVarKeys := []string{
		"STACKIT_SERVICE_ACCOUNT",
		"STACKIT_PARENT_CONTAINER_ID",
		"STACKIT_PROJECT_ID",
		"STACKIT_RESOURCE_MANAGER_ENDPOINT",
		"STACKIT_SERVICE_ACCOUNT_ENDPOINT",
		"STACKIT_AUTHORIZATION_ENDPOINT",
		"STACKIT_SKE_ENDPOINT",
	}

	DescribeTable("validates required environment variables",
		func(env map[string]string, wantErrContains string) {
			for _, key := range envVarKeys {
				setEnvVar(key, "")
			}
			for key, value := range env {
				setEnvVar(key, value)
			}

			runDir := GinkgoT().TempDir()
			d := &Deployer{options: fakeOptions{runID: "run-123", runDir: runDir}}
			err := d.loadEnvironment()
			if wantErrContains != "" {
				Expect(err).To(MatchError(ContainSubstring(wantErrContains)))
				return
			}
			Expect(err).NotTo(HaveOccurred())
			Expect(d.parentContainerID).To(Equal("parent-1"))
			Expect(d.projectMemberEmail).To(Equal("owner@example.com"))
			Expect(d.projectID).To(BeEmpty())
			Expect(d.kubeconfigPath).To(Equal(filepath.Join(runDir, "kubeconfig")))
			Expect(d.serviceAccountKeyPath).To(Equal(filepath.Join(runDir, "service-account-key.json")))
		},
		Entry("missing service account", map[string]string{
			"STACKIT_PARENT_CONTAINER_ID": "parent-1",
		}, "STACKIT_SERVICE_ACCOUNT"),
		Entry("missing parent container", map[string]string{
			"STACKIT_SERVICE_ACCOUNT": validServiceAccountKey,
		}, "STACKIT_PARENT_CONTAINER_ID"),
		Entry("invalid service account key", map[string]string{
			"STACKIT_SERVICE_ACCOUNT":     "{}",
			"STACKIT_PARENT_CONTAINER_ID": "parent-1",
		}, "invalid STACKIT_SERVICE_ACCOUNT"),
		Entry("project id no longer required", map[string]string{
			"STACKIT_SERVICE_ACCOUNT":     validServiceAccountKey,
			"STACKIT_PARENT_CONTAINER_ID": "parent-1",
		}, ""),
	)

	It("reads optional endpoints", func() {
		for _, key := range envVarKeys {
			setEnvVar(key, "")
		}
		setEnvVar("STACKIT_SERVICE_ACCOUNT", validServiceAccountKey)
		setEnvVar("STACKIT_PARENT_CONTAINER_ID", "parent-1")
		setEnvVar("STACKIT_RESOURCE_MANAGER_ENDPOINT", "https://resource-manager.example.com")
		setEnvVar("STACKIT_SERVICE_ACCOUNT_ENDPOINT", "https://service-account.example.com")
		setEnvVar("STACKIT_AUTHORIZATION_ENDPOINT", "https://authorization.example.com")
		setEnvVar("STACKIT_SKE_ENDPOINT", "https://ske.example.com")

		runDir := GinkgoT().TempDir()
		d := &Deployer{options: fakeOptions{runID: "run-123", runDir: runDir}}
		Expect(d.loadEnvironment()).To(Succeed())

		Expect(d.projectMemberEmail).To(Equal("owner@example.com"))
		Expect(d.resourceManagerEndpoint).To(Equal("https://resource-manager.example.com"))
		Expect(d.serviceAccountEndpoint).To(Equal("https://service-account.example.com"))
		Expect(d.authorizationEndpoint).To(Equal("https://authorization.example.com"))
		Expect(d.skeEndpoint).To(Equal("https://ske.example.com"))
	})
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

var _ = Describe("ensureManagedClusterAccess", func() {
	It("reuses cached key and skips membership write", func() {
		d := newTestDeployer()
		cachedKey := `{"credentials":{"privateKey":"cached"}}`
		Expect(os.WriteFile(d.serviceAccountKeyPath, []byte(cachedKey), 0o600)).To(Succeed())

		projectClient := &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{
				*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
			},
		}
		serviceAccountEmail := d.serviceAccountName() + "@sa.stackit.cloud"
		serviceAccountClient := &fakeServiceAccountClient{
			listResult: []serviceaccount.ServiceAccount{
				*serviceAccountFixture(serviceAccountEmail, "project-123"),
			},
		}
		authorizationClient := &fakeAuthorizationClient{
			listMembersResult: []authorization.Member{
				*authorization.NewMember(childProjectRole, serviceAccountEmail),
			},
		}
		fakeSKE := &fakeSKEClient{}
		var receivedKey string

		d.projectClient = projectClient
		d.serviceAccountClient = serviceAccountClient
		d.authorizationClient = authorizationClient
		d.serviceEnablementClient = &fakeServiceEnablementClient{getStatusResult: serviceenablement.NewServiceStatus()}
		d.skeClientFactory = func(_, serviceAccount, _ string) (skeClient, error) {
			receivedKey = serviceAccount
			return fakeSKE, nil
		}

		Expect(d.ensureManagedClusterAccess(context.Background())).To(Succeed())
		Expect(receivedKey).To(Equal(cachedKey))
		Expect(authorizationClient.addCalls).To(Equal(0))
		Expect(serviceAccountClient.createKeyCalls).To(Equal(0))
		Expect(d.projectID).To(Equal("project-123"))
	})

	It("creates key and adds membership", func() {
		d := newTestDeployer()

		projectClient := &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{
				*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
			},
		}
		serviceAccountEmail := d.serviceAccountName() + "@sa.stackit.cloud"
		serviceAccountClient := &fakeServiceAccountClient{
			listResult: []serviceaccount.ServiceAccount{
				*serviceAccountFixture(serviceAccountEmail, "project-123"),
			},
			createKeyResult: createServiceAccountKeyResponseFixture(serviceAccountEmail),
		}
		authorizationClient := &fakeAuthorizationClient{}
		var receivedKey string

		d.projectClient = projectClient
		d.serviceAccountClient = serviceAccountClient
		d.authorizationClient = authorizationClient
		d.serviceEnablementClient = &fakeServiceEnablementClient{getStatusResult: serviceenablement.NewServiceStatus()}
		d.skeClientFactory = func(_, serviceAccount, _ string) (skeClient, error) {
			receivedKey = serviceAccount
			return &fakeSKEClient{}, nil
		}

		Expect(d.ensureManagedClusterAccess(context.Background())).To(Succeed())
		Expect(authorizationClient.addCalls).To(Equal(1))
		Expect(serviceAccountClient.createKeyCalls).To(Equal(1))
		Expect(authorizationClient.lastAddedType).To(Equal(projectResourceType))
		Expect(authorizationClient.lastAddedID).To(Equal("project-123"))

		keyBytes, err := os.ReadFile(d.serviceAccountKeyPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(receivedKey).To(Equal(string(keyBytes)))
		Expect(string(keyBytes)).To(ContainSubstring(`"privateKey":"PRIVATE"`))

		info, err := os.Stat(d.serviceAccountKeyPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
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

var _ = Describe("retryWithBackoff", func() {
	It("retries until success", func() {
		calls := 0
		result, err := retryWithBackoff(context.Background(), wait.Backoff{Duration: 0, Factor: 1, Steps: 3}, func() (string, error) {
			calls++
			if calls < 2 {
				return "", errors.New("transient")
			}
			return "done", nil
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("done"))
		Expect(calls).To(Equal(2))
	})

	It("returns last error when exhausted", func() {
		calls := 0
		_, err := retryWithBackoff(context.Background(), wait.Backoff{Duration: 0, Factor: 1, Steps: 3}, func() (int, error) {
			calls++
			return 0, errors.New("always fails")
		})
		Expect(err).To(HaveOccurred())
		Expect(calls).To(Equal(3))
	})
})

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
				*authorization.NewMember(childProjectRole, serviceAccountEmail),
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

	It("deletes project without touching cluster delete", func() {
		d := newTestDeployer()
		fakeSKE := &fakeSKEClient{}
		d.skeClient = fakeSKE
		d.projectClient = &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{
				*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
			},
		}

		Expect(d.Down()).To(Succeed())
		projectClient := d.projectClient.(*fakeProjectClient)
		Expect(projectClient.deleteCalls).To(Equal(1))
		Expect(projectClient.waitDeletedCalls).To(Equal(1))
		Expect(fakeSKE.deleteClusterCalled).To(BeFalse())
		Expect(fakeSKE.waitDeletedCalled).To(BeFalse())
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

func newTestDeployer() *Deployer {
	runDir := GinkgoT().TempDir()
	return &Deployer{
		options:               fakeOptions{runID: "run-123", runDir: runDir},
		region:                defaultRegion,
		parentContainerID:     "parent-123",
		projectMemberEmail:    "owner@example.com",
		kubeconfigPath:        filepath.Join(runDir, "kubeconfig"),
		serviceAccountKeyPath: filepath.Join(runDir, "service-account-key.json"),
	}
}

func configureValidUpInputs(d *Deployer) {
	d.kubernetesVersion = "1.31.0"
	d.availabilityZone = defaultAvailabilityZone
	d.machineType = "g1.2"
	d.nodeImageName = "ubuntu"
	d.nodeImageVersion = "v1"
	d.nodepoolName = defaultNodepoolName
	d.volumeType = "storage"
}

func setEnvVar(key, value string) {
	Expect(os.Setenv(key, value)).To(Succeed())
	DeferCleanup(os.Unsetenv, key)
}

func providerOptionsFixture() *ske.ProviderOptions {
	kubernetesVersion := ske.NewKubernetesVersion()
	kubernetesVersion.SetVersion("1.31.0")

	availabilityZone := ske.NewAvailabilityZone()
	availabilityZone.SetName(defaultAvailabilityZone)

	machineType := ske.NewMachineType()
	machineType.SetName("g1.2")

	imageVersion := ske.NewMachineImageVersion()
	imageVersion.SetVersion("v1")

	machineImage := ske.NewMachineImage()
	machineImage.SetName("ubuntu")
	machineImage.SetVersions([]ske.MachineImageVersion{*imageVersion})

	volumeType := ske.NewVolumeType()
	volumeType.SetName("storage")

	providerOptions := ske.NewProviderOptions()
	providerOptions.SetKubernetesVersions([]ske.KubernetesVersion{*kubernetesVersion})
	providerOptions.SetAvailabilityZones([]ske.AvailabilityZone{*availabilityZone})
	providerOptions.SetMachineTypes([]ske.MachineType{*machineType})
	providerOptions.SetMachineImages([]ske.MachineImage{*machineImage})
	providerOptions.SetVolumeTypes([]ske.VolumeType{*volumeType})
	return providerOptions
}

func healthyClusterFixture() *ske.Cluster {
	cluster := ske.NewClusterWithDefaults()
	status := ske.NewClusterStatus()
	status.SetAggregated(ske.CLUSTERSTATUSSTATE_HEALTHY)
	cluster.SetStatus(*status)
	return cluster
}

func disabledServiceStatusFixture() *serviceenablement.ServiceStatus {
	status := serviceenablement.NewServiceStatus()
	state := serviceenablement.SERVICESTATUSSTATE_DISABLED
	status.State = &state
	return status
}

func projectFixture(name, projectID, containerID string, labels map[string]string) *resourcemanager.Project {
	project := resourcemanager.NewProjectWithDefaults()
	project.SetName(name)
	project.SetProjectId(projectID)
	project.SetContainerId(containerID)
	project.SetLabels(labels)
	return project
}

func projectResponseFixture(name, projectID, containerID string, labels map[string]string) *resourcemanager.GetProjectResponse {
	project := resourcemanager.NewGetProjectResponseWithDefaults()
	project.SetName(name)
	project.SetProjectId(projectID)
	project.SetContainerId(containerID)
	project.SetLabels(labels)
	return project
}

func serviceAccountFixture(email, projectID string) *serviceaccount.ServiceAccount {
	serviceAccountObject := serviceaccount.NewServiceAccountWithDefaults()
	serviceAccountObject.SetEmail(email)
	serviceAccountObject.SetProjectId(projectID)
	serviceAccountObject.SetId("service-account-id")
	serviceAccountObject.SetInternal(false)
	return serviceAccountObject
}

func createServiceAccountKeyResponseFixture(email string) *serviceaccount.CreateServiceAccountKeyResponse {
	credentials := serviceaccount.NewCreateServiceAccountKeyResponseCredentials(
		"https://accounts.stackit.cloud",
		email,
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
	)
	credentials.SetPrivateKey("PRIVATE")
	credentials.SetTokenEndpoint("https://accounts.stackit.cloud/oauth/v2/token")

	return serviceaccount.NewCreateServiceAccountKeyResponse(
		true,
		time.Unix(0, 0).UTC(),
		*credentials,
		"00000000-0000-0000-0000-000000000003",
		serviceaccount.CREATESERVICEACCOUNTKEYRESPONSEKEYALGORITHM_RSA_2048,
		serviceaccount.CREATESERVICEACCOUNTKEYRESPONSEKEYORIGIN_GENERATED,
		serviceaccount.CREATESERVICEACCOUNTKEYRESPONSEKEYTYPE_USER_MANAGED,
		"PUBLIC KEY",
	)
}

var _ types.Options = fakeOptions{}
