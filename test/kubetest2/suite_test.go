package kubetest2

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	authorization "github.com/stackitcloud/stackit-sdk-go/services/authorization/v2api"
	resourcemanager "github.com/stackitcloud/stackit-sdk-go/services/resourcemanager/v0api"
	serviceaccount "github.com/stackitcloud/stackit-sdk-go/services/serviceaccount/v2api"
	serviceenablement "github.com/stackitcloud/stackit-sdk-go/services/serviceenablement/v2api"
	"github.com/stackitcloud/stackit-sdk-go/services/ske"
	"sigs.k8s.io/kubetest2/pkg/types"
)

func TestKubetest2(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kubetest2 Suite")
}

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

func newTestDeployer() *Deployer {
	runDir := GinkgoT().TempDir()
	return &Deployer{
		options:               fakeOptions{runID: "run-123", runDir: runDir},
		region:                defaultRegion,
		parentContainerID:     "parent-123",
		projectMemberEmail:    "owner@example.com",
		kubeconfigPath:        filepath.Join(runDir, "kubeconfig"),
		serviceAccountKeyPath: filepath.Join(runDir, "service-account-key.json"),
		skeClientFactory:      newSKEClient,
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
