package kubetest2

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/klog/v2"
)

func (d *Deployer) loadEnvironment() error {
	klog.Infof("Loading STACKIT environment variables")

	serviceAccount, ok := os.LookupEnv("STACKIT_SERVICE_ACCOUNT")
	if !ok || strings.TrimSpace(serviceAccount) == "" {
		return incorrectUsagef("STACKIT_SERVICE_ACCOUNT environment variable is required")
	}

	parentContainerID, ok := os.LookupEnv("STACKIT_PARENT_CONTAINER_ID")
	if !ok || strings.TrimSpace(parentContainerID) == "" {
		return incorrectUsagef("STACKIT_PARENT_CONTAINER_ID environment variable is required")
	}

	projectMemberEmail, err := extractServiceAccountEmail(serviceAccount)
	if err != nil {
		return incorrectUsagef("invalid STACKIT_SERVICE_ACCOUNT: %v", err)
	}

	d.serviceAccount = serviceAccount
	d.parentContainerID = parentContainerID
	d.projectMemberEmail = projectMemberEmail
	d.resourceManagerEndpoint = strings.TrimSpace(os.Getenv("STACKIT_RESOURCE_MANAGER_ENDPOINT"))
	d.serviceAccountEndpoint = strings.TrimSpace(os.Getenv("STACKIT_SERVICE_ACCOUNT_ENDPOINT"))
	d.authorizationEndpoint = strings.TrimSpace(os.Getenv("STACKIT_AUTHORIZATION_ENDPOINT"))
	d.skeEndpoint = strings.TrimSpace(os.Getenv("STACKIT_SKE_ENDPOINT"))
	d.kubeconfigPath = filepath.Join(d.options.RunDir(), "kubeconfig")
	d.serviceAccountKeyPath = filepath.Join(d.options.RunDir(), "service-account-key.json")

	klog.Infof(
		"Loaded STACKIT environment: parent_container_id=%q project_member_email=%q service_account_bytes=%d kubeconfig_path=%q service_account_key_path=%q",
		d.parentContainerID,
		d.projectMemberEmail,
		len(d.serviceAccount),
		d.kubeconfigPath,
		d.serviceAccountKeyPath,
	)

	klog.Infof(
		"STACKIT API endpoint overrides: resource_manager=%q service_account=%q authorization=%q ske=%q",
		d.resourceManagerEndpoint,
		d.serviceAccountEndpoint,
		d.authorizationEndpoint,
		d.skeEndpoint,
	)

	return nil
}

func extractServiceAccountEmail(serviceAccountKey string) (string, error) {
	var key serviceAccountKeyFile
	if err := json.Unmarshal([]byte(serviceAccountKey), &key); err != nil {
		return "", fmt.Errorf("parse service account key: %w", err)
	}
	email := strings.TrimSpace(key.Credentials.Iss)
	if email == "" {
		return "", fmt.Errorf("service account key has no email in credentials.iss")
	}
	return email, nil
}
