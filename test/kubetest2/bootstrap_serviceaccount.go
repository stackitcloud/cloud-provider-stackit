package kubetest2

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	authorization "github.com/stackitcloud/stackit-sdk-go/services/authorization/v2api"
	serviceaccount "github.com/stackitcloud/stackit-sdk-go/services/serviceaccount/v2api"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

var childKeyCreationRetryBackoff = wait.Backoff{
	Duration: 3 * time.Second,
	Factor:   2.0,
	Steps:    5,
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

var childProjectRoles = []string{
	childProjectSKERole,
	childProjectStorageRole,
}

// ensureServiceAccount idempotently resolves (or creates) the managed child
// service account, grants it the required project roles, caches a
// service-account key and initializes the SKE client from it. Depends on
// d.projectID being set.
func (d *Deployer) ensureServiceAccount(ctx context.Context) error {
	childServiceAccount, err := d.resolveManagedServiceAccount(ctx, d.projectID)
	if err != nil {
		return err
	}

	if err := d.ensureProjectServiceAccountRoles(ctx, d.projectID, childServiceAccount.Email); err != nil {
		return err
	}

	serviceAccountKey, err := d.ensureCachedChildServiceAccountKey(ctx, d.projectID, childServiceAccount.Email)
	if err != nil {
		return err
	}
	return d.initializeSKEClient(serviceAccountKey)
}

func (d *Deployer) resolveManagedServiceAccount(ctx context.Context, projectID string) (*managedServiceAccount, error) {
	serviceAccounts, err := d.serviceAccountClient.ListServiceAccounts(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list service accounts in STACKIT project %q: %w", projectID, err)
	}

	for _, serviceAccount := range serviceAccounts {
		if !d.matchesManagedServiceAccountEmail(serviceAccount.GetEmail()) {
			continue
		}
		klog.Infof("Reusing managed service account=%q in project_id=%q", serviceAccount.GetEmail(), projectID)
		return &managedServiceAccount{
			Email:     serviceAccount.GetEmail(),
			ProjectID: serviceAccount.GetProjectId(),
		}, nil
	}

	klog.Infof("Creating managed service account=%q in project_id=%q", d.serviceAccountName(), projectID)
	createdServiceAccount, err := d.serviceAccountClient.CreateServiceAccount(ctx, projectID, d.serviceAccountName())
	if err != nil {
		return nil, fmt.Errorf("create service account %q in STACKIT project %q: %w", d.serviceAccountName(), projectID, err)
	}
	return &managedServiceAccount{
		Email:     createdServiceAccount.GetEmail(),
		ProjectID: createdServiceAccount.GetProjectId(),
	}, nil
}

func (d *Deployer) ensureProjectServiceAccountRoles(ctx context.Context, projectID, serviceAccountEmail string) error {
	members, err := d.authorizationClient.ListMembers(ctx, projectResourceType, projectID)
	if err != nil {
		return fmt.Errorf("list members for STACKIT project %q: %w", projectID, err)
	}

	assignedRoles := make(map[string]struct{}, len(childProjectRoles))
	for _, member := range members {
		if member.GetSubject() != serviceAccountEmail {
			continue
		}
		assignedRoles[member.GetRole()] = struct{}{}
	}

	missingMembers := make([]authorization.Member, 0, len(childProjectRoles))
	missingRoles := make([]string, 0, len(childProjectRoles))
	for _, role := range childProjectRoles {
		if _, ok := assignedRoles[role]; ok {
			klog.Infof("Managed service account=%q already has role=%q in project_id=%q", serviceAccountEmail, role, projectID)
			continue
		}
		missingRoles = append(missingRoles, role)
		missingMembers = append(missingMembers, *authorization.NewMember(role, serviceAccountEmail))
	}

	if len(missingMembers) == 0 {
		return nil
	}

	klog.Infof("Adding roles=%q for managed service account=%q in project_id=%q", strings.Join(missingRoles, ","), serviceAccountEmail, projectID)
	if err := d.authorizationClient.AddMembers(
		ctx,
		projectID,
		projectResourceType,
		missingMembers,
	); err != nil {
		return fmt.Errorf("add roles %q for service account %q in STACKIT project %q: %w", strings.Join(missingRoles, ","), serviceAccountEmail, projectID, err)
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
	createdKey, err := retryWithBackoff(ctx, childKeyCreationRetryBackoff, func() (*serviceaccount.CreateServiceAccountKeyResponse, error) {
		return d.serviceAccountClient.CreateServiceAccountKey(ctx, projectID, serviceAccountEmail)
	})
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

// retryWithBackoff retries fn until it succeeds or the backoff is exhausted.
// It is used for operations that may fail transiently, e.g. while a freshly
// created service account is still propagating through the STACKIT IAM and is
// not yet ready to have a key created for it.
func retryWithBackoff[T any](ctx context.Context, backoff wait.Backoff, fn func() (T, error)) (T, error) {
	var result T
	var lastErr error

	waitErr := wait.ExponentialBackoffWithContext(ctx, backoff, func(_ context.Context) (bool, error) {
		val, err := fn()
		if err != nil {
			lastErr = err
			return false, nil
		}
		result = val
		return true, nil
	})
	if waitErr != nil {
		return result, fmt.Errorf("backoff failed: %w, last error: %v", waitErr, lastErr)
	}
	return result, nil
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
	if strings.TrimSpace(d.serviceAccountKeyPath) == "" {
		return nil
	}
	return os.WriteFile(d.serviceAccountKeyPath, []byte(serviceAccountKey), 0o600)
}

func (d *Deployer) matchesManagedServiceAccountEmail(email string) bool {
	if email == "" {
		return false
	}
	localPart, _, found := strings.Cut(email, "@")
	if !found {
		return false
	}
	name := d.serviceAccountName()
	return localPart == name || strings.HasPrefix(localPart, name+"-")
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
