package kubetest2

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

func TestLoadEnvironmentValidation(t *testing.T) {
	testCases := []struct {
		name            string
		env             map[string]string
		wantErrContains string
	}{
		{
			name: "missing service account",
			env: map[string]string{
				"STACKIT_PARENT_CONTAINER_ID": "parent-1",
			},
			wantErrContains: "STACKIT_SERVICE_ACCOUNT",
		},
		{
			name: "missing parent container",
			env: map[string]string{
				"STACKIT_SERVICE_ACCOUNT": validServiceAccountKey,
			},
			wantErrContains: "STACKIT_PARENT_CONTAINER_ID",
		},
		{
			name: "invalid service account key",
			env: map[string]string{
				"STACKIT_SERVICE_ACCOUNT":     "{}",
				"STACKIT_PARENT_CONTAINER_ID": "parent-1",
			},
			wantErrContains: "invalid STACKIT_SERVICE_ACCOUNT",
		},
		{
			name: "project id no longer required",
			env: map[string]string{
				"STACKIT_SERVICE_ACCOUNT":     validServiceAccountKey,
				"STACKIT_PARENT_CONTAINER_ID": "parent-1",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{
				"STACKIT_SERVICE_ACCOUNT",
				"STACKIT_PARENT_CONTAINER_ID",
				"STACKIT_PROJECT_ID",
				"STACKIT_RESOURCE_MANAGER_ENDPOINT",
				"STACKIT_SERVICE_ACCOUNT_ENDPOINT",
				"STACKIT_AUTHORIZATION_ENDPOINT",
				"STACKIT_SKE_ENDPOINT",
			} {
				t.Setenv(key, "")
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			runDir := t.TempDir()
			d := &Deployer{options: fakeOptions{runID: "run-123", runDir: runDir}}
			err := d.loadEnvironment()
			if tc.wantErrContains != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErrContains, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadEnvironment() error = %v", err)
			}
			if d.parentContainerID != "parent-1" {
				t.Fatalf("parentContainerID = %q, want %q", d.parentContainerID, "parent-1")
			}
			if d.projectMemberEmail != "owner@example.com" {
				t.Fatalf("projectMemberEmail = %q, want %q", d.projectMemberEmail, "owner@example.com")
			}
			if d.projectID != "" {
				t.Fatalf("projectID = %q, want empty", d.projectID)
			}
			if d.kubeconfigPath != filepath.Join(runDir, "kubeconfig") {
				t.Fatalf("kubeconfigPath = %q", d.kubeconfigPath)
			}
			if d.serviceAccountKeyPath != filepath.Join(runDir, "service-account-key.json") {
				t.Fatalf("serviceAccountKeyPath = %q", d.serviceAccountKeyPath)
			}
		})
	}
}

func TestLoadEnvironmentReadsOptionalEndpoints(t *testing.T) {
	for _, key := range []string{
		"STACKIT_SERVICE_ACCOUNT",
		"STACKIT_PARENT_CONTAINER_ID",
		"STACKIT_RESOURCE_MANAGER_ENDPOINT",
		"STACKIT_SERVICE_ACCOUNT_ENDPOINT",
		"STACKIT_AUTHORIZATION_ENDPOINT",
		"STACKIT_SKE_ENDPOINT",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("STACKIT_SERVICE_ACCOUNT", validServiceAccountKey)
	t.Setenv("STACKIT_PARENT_CONTAINER_ID", "parent-1")
	t.Setenv("STACKIT_RESOURCE_MANAGER_ENDPOINT", "https://resource-manager.example.com")
	t.Setenv("STACKIT_SERVICE_ACCOUNT_ENDPOINT", "https://service-account.example.com")
	t.Setenv("STACKIT_AUTHORIZATION_ENDPOINT", "https://authorization.example.com")
	t.Setenv("STACKIT_SKE_ENDPOINT", "https://ske.example.com")

	runDir := t.TempDir()
	d := &Deployer{options: fakeOptions{runID: "run-123", runDir: runDir}}
	if err := d.loadEnvironment(); err != nil {
		t.Fatalf("loadEnvironment() error = %v", err)
	}

	if d.projectMemberEmail != "owner@example.com" {
		t.Fatalf("projectMemberEmail = %q, want %q", d.projectMemberEmail, "owner@example.com")
	}
	if d.resourceManagerEndpoint != "https://resource-manager.example.com" {
		t.Fatalf("resourceManagerEndpoint = %q", d.resourceManagerEndpoint)
	}
	if d.serviceAccountEndpoint != "https://service-account.example.com" {
		t.Fatalf("serviceAccountEndpoint = %q", d.serviceAccountEndpoint)
	}
	if d.authorizationEndpoint != "https://authorization.example.com" {
		t.Fatalf("authorizationEndpoint = %q", d.authorizationEndpoint)
	}
	if d.skeEndpoint != "https://ske.example.com" {
		t.Fatalf("skeEndpoint = %q", d.skeEndpoint)
	}
}

func TestResolveManagedProjectCreatesWhenMissing(t *testing.T) {
	d := newTestDeployer(t)
	projectClient := &fakeProjectClient{
		createProjectResult: projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
		waitActiveResult:    projectResponseFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
	}
	d.projectClient = projectClient

	project, err := d.resolveManagedProject(context.Background())
	if err != nil {
		t.Fatalf("resolveManagedProject() error = %v", err)
	}
	if project.ProjectID != "project-123" {
		t.Fatalf("project id = %q, want %q", project.ProjectID, "project-123")
	}
	if projectClient.createCalls != 1 {
		t.Fatalf("createCalls = %d, want 1", projectClient.createCalls)
	}
	if projectClient.waitActiveCalls != 1 {
		t.Fatalf("waitActiveCalls = %d, want 1", projectClient.waitActiveCalls)
	}
	if projectClient.lastCreateParentContainerID != d.parentContainerID {
		t.Fatalf("parent container = %q, want %q", projectClient.lastCreateParentContainerID, d.parentContainerID)
	}
	if projectClient.lastCreateName != d.projectName() {
		t.Fatalf("created project name = %q, want %q", projectClient.lastCreateName, d.projectName())
	}
	if projectClient.lastCreateOwnerEmail != d.projectMemberEmail {
		t.Fatalf("created owner email = %q, want %q", projectClient.lastCreateOwnerEmail, d.projectMemberEmail)
	}
	if !reflect.DeepEqual(projectClient.lastCreateLabels, d.managedProjectLabels()) {
		t.Fatalf("created labels = %#v, want %#v", projectClient.lastCreateLabels, d.managedProjectLabels())
	}
}

func TestResolveManagedProjectErrorsOnMultipleMatches(t *testing.T) {
	d := newTestDeployer(t)
	d.projectClient = &fakeProjectClient{
		listProjectsResult: []resourcemanager.Project{
			*projectFixture(d.projectName(), "project-1", "container-1", d.managedProjectLabels()),
			*projectFixture(d.projectName(), "project-2", "container-2", d.managedProjectLabels()),
		},
	}

	_, err := d.findManagedProject(context.Background())
	if err == nil || !strings.Contains(err.Error(), "found 2 managed STACKIT projects") {
		t.Fatalf("expected duplicate managed project error, got %v", err)
	}
}

func TestEnsureManagedClusterAccessReusesCachedKeyAndSkipsMembershipWrite(t *testing.T) {
	d := newTestDeployer(t)
	cachedKey := `{"credentials":{"privateKey":"cached"}}`
	if err := os.WriteFile(d.serviceAccountKeyPath, []byte(cachedKey), 0o600); err != nil {
		t.Fatalf("write cached key: %v", err)
	}

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

	if err := d.ensureManagedClusterAccess(context.Background()); err != nil {
		t.Fatalf("ensureManagedClusterAccess() error = %v", err)
	}
	if receivedKey != cachedKey {
		t.Fatalf("received key = %q, want cached key", receivedKey)
	}
	if authorizationClient.addCalls != 0 {
		t.Fatalf("addCalls = %d, want 0", authorizationClient.addCalls)
	}
	if serviceAccountClient.createKeyCalls != 0 {
		t.Fatalf("createKeyCalls = %d, want 0", serviceAccountClient.createKeyCalls)
	}
	if d.projectID != "project-123" {
		t.Fatalf("projectID = %q, want %q", d.projectID, "project-123")
	}
}

func TestEnsureManagedClusterAccessCreatesKeyAndAddsMembership(t *testing.T) {
	d := newTestDeployer(t)

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

	if err := d.ensureManagedClusterAccess(context.Background()); err != nil {
		t.Fatalf("ensureManagedClusterAccess() error = %v", err)
	}
	if authorizationClient.addCalls != 1 {
		t.Fatalf("addCalls = %d, want 1", authorizationClient.addCalls)
	}
	if serviceAccountClient.createKeyCalls != 1 {
		t.Fatalf("createKeyCalls = %d, want 1", serviceAccountClient.createKeyCalls)
	}
	if authorizationClient.lastAddedType != projectResourceType || authorizationClient.lastAddedID != "project-123" {
		t.Fatalf("AddMembers called with type=%q id=%q", authorizationClient.lastAddedType, authorizationClient.lastAddedID)
	}
	keyBytes, err := os.ReadFile(d.serviceAccountKeyPath)
	if err != nil {
		t.Fatalf("read cached key: %v", err)
	}
	if receivedKey != string(keyBytes) {
		t.Fatalf("factory key mismatch")
	}
	if !strings.Contains(string(keyBytes), "\"privateKey\":\"PRIVATE\"") {
		t.Fatalf("cached key did not contain serialized private key: %s", string(keyBytes))
	}
	info, err := os.Stat(d.serviceAccountKeyPath)
	if err != nil {
		t.Fatalf("stat cached key: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cached key mode = %o, want 600", info.Mode().Perm())
	}
}

func TestEnsureSKEServiceEnabledSkipsWhenAlreadyEnabled(t *testing.T) {
	d := newTestDeployer(t)
	client := &fakeServiceEnablementClient{getStatusResult: serviceenablement.NewServiceStatus()}
	d.serviceEnablementClient = client

	if err := d.ensureSKEServiceEnabled(context.Background(), "project-123"); err != nil {
		t.Fatalf("ensureSKEServiceEnabled() error = %v", err)
	}
	if client.enableCalls != 0 {
		t.Fatalf("enableCalls = %d, want 0", client.enableCalls)
	}
	if client.waitCalls != 0 {
		t.Fatalf("waitCalls = %d, want 0", client.waitCalls)
	}
	if client.lastGetStatusServiceID != skeServiceID {
		t.Fatalf("service ID = %q, want %q", client.lastGetStatusServiceID, skeServiceID)
	}
}

func TestEnsureSKEServiceEnabledEnablesWhenNotFound(t *testing.T) {
	d := newTestDeployer(t)
	client := &fakeServiceEnablementClient{
		getStatusErr: &oapierror.GenericOpenAPIError{StatusCode: http.StatusNotFound},
	}
	d.serviceEnablementClient = client

	if err := d.ensureSKEServiceEnabled(context.Background(), "project-123"); err != nil {
		t.Fatalf("ensureSKEServiceEnabled() error = %v", err)
	}
	if client.enableCalls != 1 {
		t.Fatalf("enableCalls = %d, want 1", client.enableCalls)
	}
	if client.waitCalls != 1 {
		t.Fatalf("waitCalls = %d, want 1", client.waitCalls)
	}
	if client.lastEnableProjectID != "project-123" || client.lastEnableServiceID != skeServiceID {
		t.Fatalf("EnableService called with project_id=%q service_id=%q", client.lastEnableProjectID, client.lastEnableServiceID)
	}
}

func TestEnsureSKEServiceEnabledEnablesWhenDisabled(t *testing.T) {
	d := newTestDeployer(t)
	client := &fakeServiceEnablementClient{
		getStatusResult: disabledServiceStatusFixture(),
	}
	d.serviceEnablementClient = client

	if err := d.ensureSKEServiceEnabled(context.Background(), "project-123"); err != nil {
		t.Fatalf("ensureSKEServiceEnabled() error = %v", err)
	}
	if client.enableCalls != 1 {
		t.Fatalf("enableCalls = %d, want 1", client.enableCalls)
	}
	if client.waitCalls != 1 {
		t.Fatalf("waitCalls = %d, want 1", client.waitCalls)
	}
}

func TestEnsureSKEServiceEnabledFailsOnGetStatusError(t *testing.T) {
	d := newTestDeployer(t)
	client := &fakeServiceEnablementClient{
		getStatusErr: &oapierror.GenericOpenAPIError{StatusCode: http.StatusForbidden},
	}
	d.serviceEnablementClient = client

	if err := d.ensureSKEServiceEnabled(context.Background(), "project-123"); err == nil {
		t.Fatal("ensureSKEServiceEnabled() error = nil, want non-nil")
	}
	if client.enableCalls != 0 {
		t.Fatalf("enableCalls = %d, want 0", client.enableCalls)
	}
}

func disabledServiceStatusFixture() *serviceenablement.ServiceStatus {
	status := serviceenablement.NewServiceStatus()
	state := serviceenablement.SERVICESTATUSSTATE_DISABLED
	status.State = &state
	return status
}

func TestRetryWithBackoffRetriesUntilSuccess(t *testing.T) {
	calls := 0
	result, err := retryWithBackoff(context.Background(), wait.Backoff{Duration: 0, Factor: 1, Steps: 3}, func() (string, error) {
		calls++
		if calls < 2 {
			return "", errors.New("transient")
		}
		return "done", nil
	})
	if err != nil {
		t.Fatalf("retryWithBackoff() error = %v", err)
	}
	if result != "done" {
		t.Fatalf("result = %q, want %q", result, "done")
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestRetryWithBackoffReturnsLastErrorWhenExhausted(t *testing.T) {
	calls := 0
	_, err := retryWithBackoff(context.Background(), wait.Backoff{Duration: 0, Factor: 1, Steps: 3}, func() (int, error) {
		calls++
		return 0, errors.New("always fails")
	})
	if err == nil {
		t.Fatal("retryWithBackoff() error = nil, want non-nil")
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestUpUsesDiscoveredProjectAndWritesKubeconfig(t *testing.T) {
	d := newTestDeployer(t)
	configureValidUpInputs(d)

	cachedKey := `{"credentials":{"privateKey":"cached"}}`
	if err := os.WriteFile(d.serviceAccountKeyPath, []byte(cachedKey), 0o600); err != nil {
		t.Fatalf("write cached key: %v", err)
	}

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
		if serviceAccount != cachedKey {
			t.Fatalf("unexpected service account key passed to SKE client")
		}
		return fakeSKE, nil
	}

	if err := d.Up(); err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	if fakeSKE.lastCreateProjectID != "project-123" {
		t.Fatalf("CreateOrUpdate projectID = %q, want %q", fakeSKE.lastCreateProjectID, "project-123")
	}
	if fakeSKE.lastCreateClusterName != d.clusterName() {
		t.Fatalf("cluster name = %q, want %q", fakeSKE.lastCreateClusterName, d.clusterName())
	}
	kubeconfigBytes, err := os.ReadFile(d.kubeconfigPath)
	if err != nil {
		t.Fatalf("read kubeconfig: %v", err)
	}
	if string(kubeconfigBytes) != "apiVersion: v1\n" {
		t.Fatalf("kubeconfig = %q", string(kubeconfigBytes))
	}
}

func TestDownMissingProjectIsSuccess(t *testing.T) {
	d := newTestDeployer(t)
	projectClient := &fakeProjectClient{}
	d.projectClient = projectClient

	if err := d.Down(); err != nil {
		t.Fatalf("Down() error = %v", err)
	}
	if projectClient.deleteCalls != 0 {
		t.Fatalf("deleteCalls = %d, want 0", projectClient.deleteCalls)
	}
}

func TestDownDeletesProjectWithoutTouchingClusterDelete(t *testing.T) {
	d := newTestDeployer(t)
	fakeSKE := &fakeSKEClient{}
	d.skeClient = fakeSKE
	d.projectClient = &fakeProjectClient{
		listProjectsResult: []resourcemanager.Project{
			*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
		},
	}

	if err := d.Down(); err != nil {
		t.Fatalf("Down() error = %v", err)
	}
	if d.projectClient.(*fakeProjectClient).deleteCalls != 1 {
		t.Fatalf("deleteCalls = %d, want 1", d.projectClient.(*fakeProjectClient).deleteCalls)
	}
	if d.projectClient.(*fakeProjectClient).waitDeletedCalls != 1 {
		t.Fatalf("waitDeletedCalls = %d, want 1", d.projectClient.(*fakeProjectClient).waitDeletedCalls)
	}
	if fakeSKE.deleteClusterCalled || fakeSKE.waitDeletedCalled {
		t.Fatalf("cluster delete path was invoked unexpectedly")
	}
}

func TestIsUpNoProjectReturnsFalse(t *testing.T) {
	d := newTestDeployer(t)
	d.projectClient = &fakeProjectClient{}

	isUp, err := d.IsUp()
	if err != nil {
		t.Fatalf("IsUp() error = %v", err)
	}
	if isUp {
		t.Fatalf("IsUp() = true, want false")
	}
}

func TestIsUpProjectWithoutCachedKeyReturnsError(t *testing.T) {
	d := newTestDeployer(t)
	d.projectClient = &fakeProjectClient{
		listProjectsResult: []resourcemanager.Project{
			*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
		},
	}

	isUp, err := d.IsUp()
	if err == nil || !strings.Contains(err.Error(), "child service-account key cache") {
		t.Fatalf("expected missing key cache error, got up=%v err=%v", isUp, err)
	}
}

func TestIsUpProjectWithCachedKeyQueriesCluster(t *testing.T) {
	d := newTestDeployer(t)
	d.projectClient = &fakeProjectClient{
		listProjectsResult: []resourcemanager.Project{
			*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
		},
	}
	cachedKey := `{"credentials":{"privateKey":"cached"}}`
	if err := os.WriteFile(d.serviceAccountKeyPath, []byte(cachedKey), 0o600); err != nil {
		t.Fatalf("write cached key: %v", err)
	}
	fakeSKE := &fakeSKEClient{
		getClusterResult: healthyClusterFixture(),
	}
	d.skeClientFactory = func(_, serviceAccount, _ string) (skeClient, error) {
		if serviceAccount != cachedKey {
			t.Fatalf("unexpected cached key content")
		}
		return fakeSKE, nil
	}

	isUp, err := d.IsUp()
	if err != nil {
		t.Fatalf("IsUp() error = %v", err)
	}
	if !isUp {
		t.Fatalf("IsUp() = false, want true")
	}
	if fakeSKE.lastGetProjectID != "project-123" {
		t.Fatalf("GetCluster projectID = %q, want %q", fakeSKE.lastGetProjectID, "project-123")
	}
}

func newTestDeployer(t *testing.T) *Deployer {
	t.Helper()
	runDir := t.TempDir()
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

func TestIsUpReturnsFalseWhenClusterMissing(t *testing.T) {
	d := newTestDeployer(t)
	d.projectClient = &fakeProjectClient{
		listProjectsResult: []resourcemanager.Project{
			*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
		},
	}
	cachedKey := `{"credentials":{"privateKey":"cached"}}`
	if err := os.WriteFile(d.serviceAccountKeyPath, []byte(cachedKey), 0o600); err != nil {
		t.Fatalf("write cached key: %v", err)
	}
	fakeSKE := &fakeSKEClient{
		getClusterErr: &oapierror.GenericOpenAPIError{StatusCode: http.StatusNotFound},
	}
	d.skeClientFactory = func(_, _, _ string) (skeClient, error) {
		return fakeSKE, nil
	}

	isUp, err := d.IsUp()
	if err != nil {
		t.Fatalf("IsUp() error = %v", err)
	}
	if isUp {
		t.Fatalf("IsUp() = true, want false")
	}
}

var _ types.Options = fakeOptions{}
