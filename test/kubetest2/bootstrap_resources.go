package kubetest2

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	authorization "github.com/stackitcloud/stackit-sdk-go/services/authorization/v2api"
	resourcemanager "github.com/stackitcloud/stackit-sdk-go/services/resourcemanager/v0api"
	serviceaccount "github.com/stackitcloud/stackit-sdk-go/services/serviceaccount/v2api"
	"k8s.io/klog/v2"
)

type managedProject struct {
	ContainerID string
	ProjectID   string
	Name        string
	Labels      map[string]string
}

type managedServiceAccount struct {
	Email     string
	ProjectID string
}

type serviceAccountKeyFile struct {
	Active       bool                             `json:"active"`
	CreatedAt    time.Time                        `json:"createdAt"`
	Credentials  serviceAccountKeyCredentialsFile `json:"credentials"`
	ID           string                           `json:"id"`
	KeyAlgorithm string                           `json:"keyAlgorithm"`
	KeyOrigin    string                           `json:"keyOrigin"`
	KeyType      string                           `json:"keyType"`
	PublicKey    string                           `json:"publicKey"`
	ValidUntil   *time.Time                       `json:"validUntil,omitempty"`
}

type serviceAccountKeyCredentialsFile struct {
	Aud           string  `json:"aud"`
	Iss           string  `json:"iss"`
	Kid           string  `json:"kid"`
	PrivateKey    *string `json:"privateKey,omitempty"`
	Sub           string  `json:"sub"`
	TokenEndpoint string  `json:"tokenEndpoint"`
}

func (d *Deployer) initializeBootstrapClients() error {
	if d.projectClient == nil {
		client, err := newProjectClient(d.serviceAccount)
		if err != nil {
			return err
		}
		d.projectClient = client
	}
	if d.serviceAccountClient == nil {
		client, err := newServiceAccountClient(d.serviceAccount)
		if err != nil {
			return err
		}
		d.serviceAccountClient = client
	}
	if d.authorizationClient == nil {
		client, err := newAuthorizationClient(d.serviceAccount)
		if err != nil {
			return err
		}
		d.authorizationClient = client
	}
	return nil
}

func (d *Deployer) initializeSKEClient(serviceAccountKey string) error {
	if d.skeClientFactory == nil {
		d.skeClientFactory = newSKEClient
	}
	client, err := d.skeClientFactory(d.region, serviceAccountKey)
	if err != nil {
		return err
	}
	d.skeClient = client
	return nil
}

func (d *Deployer) ensureManagedClusterAccess(ctx context.Context) error {
	project, err := d.resolveManagedProject(ctx)
	if err != nil {
		return err
	}
	d.projectID = project.ProjectID

	childServiceAccount, err := d.resolveManagedServiceAccount(ctx, project.ProjectID)
	if err != nil {
		return err
	}
	d.childServiceAccountEmail = childServiceAccount.Email

	if err := d.ensureProjectServiceAccountRole(ctx, project.ProjectID, childServiceAccount.Email); err != nil {
		return err
	}

	serviceAccountKey, err := d.ensureCachedChildServiceAccountKey(ctx, project.ProjectID, childServiceAccount.Email)
	if err != nil {
		return err
	}
	return d.initializeSKEClient(serviceAccountKey)
}

func (d *Deployer) findManagedProject(ctx context.Context) (*managedProject, error) {
	projects, err := d.projectClient.ListProjects(ctx, d.parentContainerID)
	if err != nil {
		return nil, fmt.Errorf("list STACKIT projects under parent container %q: %w", d.parentContainerID, err)
	}

	matches := make([]managedProject, 0, 1)
	for i := range projects {
		project := &projects[i]
		if !d.matchesManagedProject(project) {
			continue
		}
		matches = append(matches, managedProject{
			ContainerID: project.GetContainerId(),
			ProjectID:   project.GetProjectId(),
			Name:        project.GetName(),
			Labels:      project.GetLabels(),
		})
	}

	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf(
			"found %d managed STACKIT projects for run token %q under parent container %q",
			len(matches),
			d.runToken(),
			d.parentContainerID,
		)
	}
}

func (d *Deployer) resolveManagedProject(ctx context.Context) (*managedProject, error) {
	project, err := d.findManagedProject(ctx)
	if err != nil {
		return nil, err
	}
	if project != nil {
		klog.Infof("Reusing managed project=%q project_id=%q", project.Name, project.ProjectID)
		return project, nil
	}

	klog.Infof("Creating managed project=%q under parent_container_id=%q", d.projectName(), d.parentContainerID)
	createdProject, err := d.projectClient.CreateProject(
		ctx,
		d.parentContainerID,
		d.projectName(),
		d.projectMemberEmail,
		d.managedProjectLabels(),
	)
	if err != nil {
		return nil, fmt.Errorf("create STACKIT project %q: %w", d.projectName(), err)
	}

	activeProject, err := d.projectClient.WaitForProjectActive(ctx, createdProject.GetContainerId())
	if err != nil {
		return nil, fmt.Errorf("wait for STACKIT project %q to become active: %w", createdProject.GetProjectId(), err)
	}

	return &managedProject{
		ContainerID: activeProject.GetContainerId(),
		ProjectID:   activeProject.GetProjectId(),
		Name:        activeProject.GetName(),
		Labels:      activeProject.GetLabels(),
	}, nil
}

func (d *Deployer) resolveManagedServiceAccount(ctx context.Context, projectID string) (*managedServiceAccount, error) {
	serviceAccounts, err := d.serviceAccountClient.ListServiceAccounts(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list service accounts in STACKIT project %q: %w", projectID, err)
	}

	matches := make([]managedServiceAccount, 0, 1)
	for _, serviceAccount := range serviceAccounts {
		if !d.matchesManagedServiceAccountEmail(serviceAccount.GetEmail()) {
			continue
		}
		matches = append(matches, managedServiceAccount{
			Email:     serviceAccount.GetEmail(),
			ProjectID: serviceAccount.GetProjectId(),
		})
	}

	switch len(matches) {
	case 0:
		klog.Infof("Creating managed service account=%q in project_id=%q", d.serviceAccountName(), projectID)
		createdServiceAccount, err := d.serviceAccountClient.CreateServiceAccount(ctx, projectID, d.serviceAccountName())
		if err != nil {
			return nil, fmt.Errorf("create service account %q in STACKIT project %q: %w", d.serviceAccountName(), projectID, err)
		}
		return &managedServiceAccount{
			Email:     createdServiceAccount.GetEmail(),
			ProjectID: createdServiceAccount.GetProjectId(),
		}, nil
	case 1:
		klog.Infof("Reusing managed service account=%q in project_id=%q", matches[0].Email, projectID)
		return &matches[0], nil
	default:
		return nil, fmt.Errorf(
			"found %d managed service accounts for run token %q in project %q",
			len(matches),
			d.runToken(),
			projectID,
		)
	}
}

func (d *Deployer) ensureProjectServiceAccountRole(ctx context.Context, projectID, serviceAccountEmail string) error {
	members, err := d.authorizationClient.ListMembers(ctx, projectResourceType, projectID)
	if err != nil {
		return fmt.Errorf("list members for STACKIT project %q: %w", projectID, err)
	}
	for _, member := range members {
		if member.GetSubject() == serviceAccountEmail && member.GetRole() == childProjectRole {
			klog.Infof("Managed service account=%q already has role=%q in project_id=%q", serviceAccountEmail, childProjectRole, projectID)
			return nil
		}
	}

	klog.Infof("Adding role=%q for managed service account=%q in project_id=%q", childProjectRole, serviceAccountEmail, projectID)
	if err := d.authorizationClient.AddMembers(
		ctx,
		projectID,
		projectResourceType,
		[]authorization.Member{*authorization.NewMember(childProjectRole, serviceAccountEmail)},
	); err != nil {
		return fmt.Errorf("add role %q for service account %q in STACKIT project %q: %w", childProjectRole, serviceAccountEmail, projectID, err)
	}
	return nil
}

func (d *Deployer) ensureCachedChildServiceAccountKey(ctx context.Context, projectID, serviceAccountEmail string) (string, error) {
	cachedKey, ok, err := d.readCachedChildServiceAccountKey()
	if err != nil {
		return "", fmt.Errorf("read cached child service-account key %q: %w", d.serviceAccountKeyPath, err)
	}
	if ok {
		klog.Infof("Reusing cached child service-account key %q", d.serviceAccountKeyPath)
		return cachedKey, nil
	}

	klog.Infof("Creating child service-account key for service_account=%q in project_id=%q", serviceAccountEmail, projectID)
	createdKey, err := d.serviceAccountClient.CreateServiceAccountKey(ctx, projectID, serviceAccountEmail)
	if err != nil {
		return "", fmt.Errorf("create service-account key for %q in STACKIT project %q: %w", serviceAccountEmail, projectID, err)
	}
	keyJSON, err := serviceAccountKeyJSON(createdKey)
	if err != nil {
		return "", fmt.Errorf("serialize service-account key for %q: %w", serviceAccountEmail, err)
	}
	if err := d.writeCachedChildServiceAccountKey(keyJSON); err != nil {
		return "", fmt.Errorf("write cached child service-account key %q: %w", d.serviceAccountKeyPath, err)
	}
	return keyJSON, nil
}

func (d *Deployer) readCachedChildServiceAccountKey() (key string, ok bool, err error) {
	if strings.TrimSpace(d.serviceAccountKeyPath) == "" {
		return "", false, nil
	}
	keyBytes, err := os.ReadFile(d.serviceAccountKeyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return string(keyBytes), true, nil
}

func (d *Deployer) writeCachedChildServiceAccountKey(serviceAccountKey string) error {
	return os.WriteFile(d.serviceAccountKeyPath, []byte(serviceAccountKey), 0o600)
}

func (d *Deployer) managedProjectLabels() map[string]string {
	return map[string]string{
		projectLabelScopeKey:   projectLabelScopeValue,
		projectLabelManagedKey: projectLabelManagedValue,
		projectLabelRunIDKey:   d.runToken(),
	}
}

func (d *Deployer) matchesManagedProject(project *resourcemanager.Project) bool {
	if project.GetName() != d.projectName() {
		return false
	}
	labels := project.GetLabels()
	if labels == nil {
		return false
	}
	return labels[projectLabelScopeKey] == projectLabelScopeValue &&
		labels[projectLabelManagedKey] == projectLabelManagedValue &&
		labels[projectLabelRunIDKey] == d.runToken()
}

func (d *Deployer) matchesManagedServiceAccountEmail(email string) bool {
	if email == "" {
		return false
	}
	localPart, _, found := strings.Cut(email, "@")
	if !found {
		return false
	}
	return strings.HasPrefix(localPart, d.serviceAccountName())
}

func serviceAccountKeyJSON(createdKey *serviceaccount.CreateServiceAccountKeyResponse) (string, error) {
	credentials := createdKey.GetCredentials()
	privateKey, ok := credentials.GetPrivateKeyOk()
	if !ok || strings.TrimSpace(*privateKey) == "" {
		return "", fmt.Errorf("service-account key response did not include a private key")
	}

	serviceAccountKey := serviceAccountKeyFile{
		Active:    createdKey.GetActive(),
		CreatedAt: createdKey.GetCreatedAt(),
		Credentials: serviceAccountKeyCredentialsFile{
			Aud:           credentials.GetAud(),
			Iss:           credentials.GetIss(),
			Kid:           credentials.GetKid(),
			PrivateKey:    privateKey,
			Sub:           credentials.GetSub(),
			TokenEndpoint: credentials.GetTokenEndpoint(),
		},
		ID:           createdKey.GetId(),
		KeyAlgorithm: string(createdKey.GetKeyAlgorithm()),
		KeyOrigin:    string(createdKey.GetKeyOrigin()),
		KeyType:      string(createdKey.GetKeyType()),
		PublicKey:    createdKey.GetPublicKey(),
	}
	if validUntil, ok := createdKey.GetValidUntilOk(); ok {
		serviceAccountKey.ValidUntil = validUntil
	}

	keyJSON, err := json.Marshal(serviceAccountKey)
	if err != nil {
		return "", err
	}
	return string(keyJSON), nil
}
