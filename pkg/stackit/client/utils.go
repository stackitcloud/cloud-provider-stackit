package client

import (
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
)

func FilterVolumes(volumes []iaas.Volume, filters map[string]string) []iaas.Volume {
	filteredVolumes := make([]iaas.Volume, 0)

	if filters == nil {
		return volumes
	}

	for i := range volumes {
		volume := &volumes[i]
		if val, ok := filters["Name"]; ok && val != volume.GetName() {
			continue
		}
		filteredVolumes = append(filteredVolumes, *volume)
	}

	return filteredVolumes
}

//nolint:dupl // We don't feel like doing generics to undupe this.
func FilterSnapshots(snapshots []iaas.Snapshot, filters map[string]string) []iaas.Snapshot {
	filteredSnapshots := make([]iaas.Snapshot, 0)

	if filters == nil {
		return snapshots
	}

	for _, obj := range snapshots {
		if val, ok := filters["Status"]; ok && val != obj.GetStatus() {
			continue
		}
		if val, ok := filters["VolumeID"]; ok && val != obj.GetVolumeId() {
			continue
		}
		if val, ok := filters["Name"]; ok && val != obj.GetName() {
			continue
		}
		filteredSnapshots = append(filteredSnapshots, obj)
	}

	return filteredSnapshots
}

//nolint:dupl // We don't feel like doing generics to undupe this.
func FilterBackups(backups []iaas.Backup, filters map[string]string) []iaas.Backup {
	filteredBackups := make([]iaas.Backup, 0)

	if filters == nil {
		return backups
	}

	for _, obj := range backups {
		if val, ok := filters["Status"]; ok && val != obj.GetStatus() {
			continue
		}
		if val, ok := filters["VolumeID"]; ok && val != obj.GetVolumeId() {
			continue
		}
		if val, ok := filters["Name"]; ok && val != obj.GetName() {
			continue
		}
		filteredBackups = append(filteredBackups, obj)
	}

	return filteredBackups
}