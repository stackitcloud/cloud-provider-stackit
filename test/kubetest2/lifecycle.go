package kubetest2

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/stackiterrors"
	"github.com/stackitcloud/stackit-sdk-go/services/ske"
	"k8s.io/klog/v2"
)

type bootstrapStep struct {
	name string
	fn   func(context.Context) error
}

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
	for _, step := range []bootstrapStep{
		{"project", d.ensureProject},
		{"service account", d.ensureServiceAccount},
		{"cluster", d.ensureCluster},
		{"csi", d.ensureCSI},
	} {
		klog.Infof("Bootstrap step %q starting", step.name)
		if err := step.fn(ctx); err != nil {
			return fmt.Errorf("bootstrap step %q: %w", step.name, err)
		}
		klog.Infof("Bootstrap step %q completed", step.name)
	}

	klog.Infof("Cluster up flow completed successfully for cluster=%q", d.clusterName())

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
		d.kubeconfigPath = filepath.Join(d.options.RunDir(), "kubeconfig")
	}
	klog.Infof("Returning kubeconfig path %q", d.kubeconfigPath)
	return d.kubeconfigPath, nil
}
