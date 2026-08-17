package kubetest2

import (
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

	projectMemberEmail, ok := os.LookupEnv("STACKIT_PROJECT_MEMBER_EMAIL")
	if !ok || strings.TrimSpace(projectMemberEmail) == "" {
		return incorrectUsagef("STACKIT_PROJECT_MEMBER_EMAIL environment variable is required")
	}

	d.serviceAccount = serviceAccount
	d.parentContainerID = parentContainerID
	d.projectMemberEmail = projectMemberEmail
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

	return nil
}
