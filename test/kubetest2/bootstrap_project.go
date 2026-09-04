package kubetest2

import (
	"context"
	"fmt"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/stackiterrors"
	resourcemanager "github.com/stackitcloud/stackit-sdk-go/services/resourcemanager/v0api"
	serviceenablement "github.com/stackitcloud/stackit-sdk-go/services/serviceenablement/v2api"
	"k8s.io/klog/v2"
)

type managedProject struct {
	ContainerID string
	ProjectID   string
	Name        string
	Labels      map[string]string
}

// ensureProject idempotently resolves (or creates) the managed STACKIT project
// and enables the SKE service for it. Sets d.projectID on success.
func (d *Deployer) ensureProject(ctx context.Context) error {
	project, err := d.resolveManagedProject(ctx)
	if err != nil {
		return err
	}
	d.projectID = project.ProjectID

	return d.ensureSKEServiceEnabled(ctx, project.ProjectID)
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

// ensureSKEServiceEnabled idempotently enables the SKE (Kubernetes Engine)
// service for the managed project and waits until it is enabled.
func (d *Deployer) ensureSKEServiceEnabled(ctx context.Context, projectID string) error {
	status, err := d.serviceEnablementClient.GetServiceStatus(ctx, d.region, projectID, skeServiceID)
	if err != nil {
		if !stackiterrors.IsNotFound(err) {
			return fmt.Errorf("get SKE service status for project %q: %w", projectID, err)
		}
		klog.Infof("SKE service not yet enabled for project_id=%q", projectID)
	} else if status.GetState() == serviceenablement.SERVICESTATUSSTATE_ENABLED {
		klog.Infof("SKE service already enabled for project_id=%q", projectID)
		return nil
	} else {
		klog.Infof("SKE service in state %q for project_id=%q, enabling", status.GetState(), projectID)
	}

	if err := d.serviceEnablementClient.EnableService(ctx, d.region, projectID, skeServiceID); err != nil {
		return fmt.Errorf("enable SKE service for project %q: %w", projectID, err)
	}
	if err := d.serviceEnablementClient.WaitForServiceEnabled(ctx, d.region, projectID, skeServiceID); err != nil {
		return fmt.Errorf("wait for SKE service enablement for project %q: %w", projectID, err)
	}
	return nil
}
