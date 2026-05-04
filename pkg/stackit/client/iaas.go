package client

import (
	"context"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
)

type IaaSClient interface {
	GetServer(ctx context.Context, serverID string) (*iaas.Server, error)
	DeleteServer(ctx context.Context, serverID string) error
	CreateServer(ctx context.Context, create iaas.CreateServerPayload) (*iaas.Server, error)
	UpdateServer(ctx context.Context, serverID string, update iaas.UpdateServerPayload) (*iaas.Server, error)
	ListServers(ctx context.Context) (*[]iaas.Server, error)

	CreateSnapshot(ctx context.Context, payload iaas.CreateSnapshotPayload) (*iaas.Snapshot, error)
	ListSnapshots(ctx context.Context) ([]iaas.Snapshot, error)
	DeleteSnapshot(ctx context.Context, snapshotID string) error
	GetSnapshot(ctx context.Context, snapshotID string) (*iaas.Snapshot, error)
	WaitSnapshotReady(ctx context.Context, snapshotID string) (*string, error)

	CreateBackup(ctx context.Context, payload iaas.CreateBackupPayload) (*iaas.Backup, error)
	ListBackups(ctx context.Context) ([]iaas.Backup, error)
	DeleteBackup(ctx context.Context, backupID string) error
	GetBackup(ctx context.Context, backupID string) (*iaas.Backup, error)
	WaitBackupReady(ctx context.Context, backupID string) (*string, error)
}

type iaasClient struct {
	Client    iaas.DefaultAPI
	projectID string
	region    string
}

func NewIaaSClient(region string, endpoints stackitv1alpha1.APIEndpoints, credentials *stackit.Credentials) (IaaSClient, error) {
	options := clientOptions(endpoints, credentials)

	if endpoints.IaaS != nil {
		options = append(options, sdkconfig.WithEndpoint(*endpoints.IaaS))
	}

	if endpoints.TokenEndpoint != nil {
		options = append(options, sdkconfig.WithTokenEndpoint(*endpoints.TokenEndpoint))
	}

	apiClient, err := iaas.NewAPIClient(options...)
	if err != nil {
		return nil, err
	}
	return &iaasClient{
		Client:    apiClient.DefaultAPI,
		projectID: credentials.ProjectID,
		region:    region,
	}, nil
}

func (i iaasClient) GetServer(ctx context.Context, serverID string) (*iaas.Server, error) {
	server, err := i.Client.GetServer(ctx, i.projectID, i.region, serverID).Execute()
	if isOpenAPINotFound(err) {
		return nil, ErrorNotFound
	}

	return server, nil
}

func (i iaasClient) DeleteServer(ctx context.Context, serverID string) error {
	return i.Client.DeleteServer(ctx, i.projectID, i.region, serverID).Execute()
}

func (i iaasClient) CreateServer(ctx context.Context, create iaas.CreateServerPayload) (*iaas.Server, error) {
	server, err := i.Client.CreateServer(ctx, i.projectID, i.region).CreateServerPayload(create).Execute()
	if isOpenAPINotFound(err) {
		return nil, ErrorNotFound
	}
	return server, err
}

func (i iaasClient) UpdateServer(ctx context.Context, serverID string, update iaas.UpdateServerPayload) (*iaas.Server, error) {
	return i.Client.UpdateServer(ctx, i.projectID, i.region, serverID).UpdateServerPayload(update).Execute()
}

func (i iaasClient) ListServers(ctx context.Context) (*[]iaas.Server, error) {
	resp, err := i.Client.ListServers(ctx, i.projectID, i.region).Details(true).Execute()
	if err != nil {
		return nil, err
	}
	return &resp.Items, nil
}

func (i iaasClient) CreateSnapshot(ctx context.Context, payload iaas.CreateSnapshotPayload) (*iaas.Snapshot, error) {
	snapshot, err := i.Client.CreateSnapshot(ctx, i.projectID, i.region).CreateSnapshotPayload(payload).Execute()
	if err != nil {
		return nil, err
	}

	return snapshot, nil
}

func (i iaasClient) ListSnapshots(ctx context.Context) ([]iaas.Snapshot, error) {
	snaps, err := i.Client.ListSnapshotsInProject(ctx, i.projectID, i.region).Execute()
	if err != nil {
		return nil, err
	}

	return snaps.Items, nil
}

func (i iaasClient) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	return i.Client.DeleteSnapshot(ctx, i.projectID, i.region, snapshotID).Execute()
}

func (i iaasClient) GetSnapshot(ctx context.Context, snapshotID string) (*iaas.Snapshot, error) {
	snapshot, err := i.Client.GetSnapshot(ctx, i.projectID, i.region, snapshotID).Execute()
	if err != nil {
		return nil, err
	}

	return snapshot, nil
}

func (i iaasClient) WaitSnapshotReady(ctx context.Context, snapshotID string) (*string, error) {
	snap, err := i.GetSnapshot(ctx, snapshotID)
	if err != nil {
		return nil, err
	}

	if snap != nil {
		return snap.Status, nil
	}

	return new("Failed to get Snapshot status"), nil
}

func (i iaasClient) CreateBackup(ctx context.Context, payload iaas.CreateBackupPayload) (*iaas.Backup, error) {
	backup, err := i.Client.CreateBackup(ctx, i.projectID, i.region).CreateBackupPayload(payload).Execute()
	if err != nil {
		return nil, err
	}

	return backup, nil
}

func (i iaasClient) ListBackups(ctx context.Context) ([]iaas.Backup, error) {
	backups, err := i.Client.ListBackups(ctx, i.projectID, i.region).Execute()
	if err != nil {
		return nil, err
	}

	return backups.Items, nil
}

func (i iaasClient) DeleteBackup(ctx context.Context, backupID string) error {
	return i.Client.DeleteBackup(ctx, i.projectID, i.region, backupID).Execute()
}

func (i iaasClient) GetBackup(ctx context.Context, backupID string) (*iaas.Backup, error) {
	backup, err := i.Client.GetBackup(ctx, i.projectID, i.region, backupID).Execute()
	if err != nil {
		return nil, err
	}

	return backup, nil
}

func (i iaasClient) WaitBackupReady(ctx context.Context, backupID string) (*string, error) {
	backup, err := i.GetBackup(ctx, backupID)
	if err != nil {
		return nil, err
	}

	if backup != nil {
		return backup.Status, nil
	}

	new("Failed to get backup status"), nil
}
