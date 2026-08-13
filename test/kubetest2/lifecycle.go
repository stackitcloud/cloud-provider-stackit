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

	client, err := newSKEClient(d.region, d.serviceAccount)
	if err != nil {
		return err
	}
	d.skeClient = client

	klog.Infof("STACKIT kubetest2 deployer initialized successfully")

	return nil
}

func (d *Deployer) Up() error {
	klog.Infof("Starting cluster up flow for cluster=%q", d.clusterName())

	ctx := context.Background()
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
	clusterName := d.clusterName()

	klog.Infof("Starting cluster down flow for cluster=%q", clusterName)

	if err := d.skeClient.DeleteCluster(ctx, d.projectID, d.region, clusterName); err != nil {
		if !stackiterrors.IsNotFound(err) {
			return fmt.Errorf("delete SKE cluster %q: %w", clusterName, err)
		}
		klog.Infof("Cluster=%q already absent, treating delete as success", clusterName)
		return nil
	}

	if err := d.skeClient.WaitForClusterDeleted(ctx, d.projectID, d.region, clusterName); err != nil {
		return fmt.Errorf("wait for SKE cluster %q deletion: %w", clusterName, err)
	}

	klog.Infof("Cluster down flow completed successfully for cluster=%q", clusterName)

	return nil
}

func (d *Deployer) IsUp() (bool, error) {
	ctx := context.Background()
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
