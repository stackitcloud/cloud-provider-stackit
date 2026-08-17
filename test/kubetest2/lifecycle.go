package kubetest2

import (
	"context"
	"fmt"
	"os"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/stackiterrors"
	"github.com/stackitcloud/stackit-sdk-go/services/ske"
	"k8s.io/klog/v2"
)

func (d *Deployer) Init() error {
	klog.Infof("Initializing STACKIT kubetest2 deployer")

	if err := d.validate(); err != nil {
		return err
	}
	if err := d.loadEnvironment(); err != nil {
		return err
	}

	if err := d.initializeBootstrapClients(); err != nil {
		return err
	}

	klog.Infof("STACKIT kubetest2 deployer initialized successfully")

	return nil
}

func (d *Deployer) Up() error {
	klog.Infof("Starting cluster up flow for cluster=%q", d.clusterName())

	ctx := context.Background()
	if err := d.ensureManagedClusterAccess(ctx); err != nil {
		return err
	}
	if err := d.validateProviderOptions(ctx); err != nil {
		return err
	}

	clusterName := d.clusterName()
	payload := d.clusterPayload()

	klog.Infof("Submitting SKE create/update request for cluster=%q", clusterName)
	if _, err := d.skeClient.CreateOrUpdateCluster(ctx, d.projectID, d.region, clusterName, payload); err != nil {
		return fmt.Errorf("create or update SKE cluster %q: %w", clusterName, err)
	}
	klog.Infof("Submitted SKE create/update request for cluster=%q", clusterName)

	if _, err := d.skeClient.WaitForClusterReady(ctx, d.projectID, d.region, clusterName); err != nil {
		return fmt.Errorf("wait for SKE cluster %q to become ready: %w", clusterName, err)
	}

	klog.Infof("Creating kubeconfig for cluster=%q", clusterName)
	kubeconfig, err := d.skeClient.CreateKubeconfig(ctx, d.projectID, d.region, clusterName, d.kubeconfigExpiresIn)
	if err != nil {
		return fmt.Errorf("create kubeconfig for SKE cluster %q: %w", clusterName, err)
	}
	klog.Infof("Writing kubeconfig for cluster=%q to %q", clusterName, d.kubeconfigPath)
	if err := os.WriteFile(d.kubeconfigPath, []byte(kubeconfig.GetKubeconfig()), 0o600); err != nil {
		return fmt.Errorf("write kubeconfig %q: %w", d.kubeconfigPath, err)
	}

	klog.Infof("Cluster up flow completed successfully for cluster=%q", clusterName)

	return nil
}

func (d *Deployer) Down() error {
	ctx := context.Background()

	klog.Infof("Starting cluster down flow for cluster=%q", d.clusterName())

	project, err := d.findManagedProject(ctx)
	if err != nil {
		return err
	}
	if project == nil {
		klog.Infof("Managed project for run_id=%q is already absent", d.options.RunID())
		return nil
	}

	d.projectID = project.ProjectID
	if err := d.projectClient.DeleteProject(ctx, project.ProjectID); err != nil {
		if !stackiterrors.IsNotFound(err) {
			return fmt.Errorf("delete STACKIT project %q: %w", project.ProjectID, err)
		}
		klog.Infof("Project=%q already absent, treating delete as success", project.ProjectID)
		return nil
	}

	if err := d.projectClient.WaitForProjectDeleted(ctx, project.ProjectID); err != nil {
		return fmt.Errorf("wait for STACKIT project %q deletion: %w", project.ProjectID, err)
	}

	klog.Infof("Cluster down flow completed successfully for project=%q", project.ProjectID)

	return nil
}

func (d *Deployer) IsUp() (bool, error) {
	ctx := context.Background()
	project, err := d.findManagedProject(ctx)
	if err != nil {
		return false, err
	}
	if project == nil {
		klog.Infof("Managed project for run_id=%q not found during IsUp check", d.options.RunID())
		return false, nil
	}

	d.projectID = project.ProjectID
	serviceAccountKey, ok, err := d.readCachedChildServiceAccountKey()
	if err != nil {
		return false, fmt.Errorf("read cached child service-account key %q: %w", d.serviceAccountKeyPath, err)
	}
	if !ok {
		return false, fmt.Errorf(
			"managed project %q exists but child service-account key cache %q is missing",
			project.ProjectID,
			d.serviceAccountKeyPath,
		)
	}
	if err := d.initializeSKEClient(serviceAccountKey); err != nil {
		return false, err
	}

	klog.Infof("Checking cluster state for cluster=%q", d.clusterName())
	cluster, err := d.skeClient.GetCluster(ctx, d.projectID, d.region, d.clusterName())
	if err != nil {
		if stackiterrors.IsNotFound(err) {
			klog.Infof("Cluster=%q not found during IsUp check", d.clusterName())
			return false, nil
		}
		return false, fmt.Errorf("get SKE cluster %q: %w", d.clusterName(), err)
	}

	if cluster.Status == nil || cluster.Status.Aggregated == nil {
		klog.Infof("Cluster=%q has no aggregated status yet", d.clusterName())
		return false, nil
	}

	state := cluster.Status.GetAggregated()
	klog.Infof("Cluster=%q current aggregated state=%q", d.clusterName(), state)
	return state == ske.CLUSTERSTATUSSTATE_HEALTHY || state == ske.CLUSTERSTATUSSTATE_HIBERNATED, nil
}

func (d *Deployer) Kubeconfig() (string, error) {
	if d.kubeconfigPath == "" {
		d.kubeconfigPath = d.options.RunDir() + "/kubeconfig"
	}
	klog.Infof("Returning kubeconfig path %q", d.kubeconfigPath)
	return d.kubeconfigPath, nil
}
