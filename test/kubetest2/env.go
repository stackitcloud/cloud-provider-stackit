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

	projectID, ok := os.LookupEnv("STACKIT_PROJECT_ID")
	if !ok || strings.TrimSpace(projectID) == "" {
		return incorrectUsagef("STACKIT_PROJECT_ID environment variable is required")
	}

	d.serviceAccount = serviceAccount
	d.projectID = projectID
	d.kubeconfigPath = filepath.Join(d.options.RunDir(), "kubeconfig")

	klog.Infof(
		"Loaded STACKIT environment: project_id=%q service_account_bytes=%d kubeconfig_path=%q",
		d.projectID,
		len(d.serviceAccount),
		d.kubeconfigPath,
	)

	return nil
}
