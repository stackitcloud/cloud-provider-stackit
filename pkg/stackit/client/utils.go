package client

import (
	"fmt"
	"strings"

	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
)

func LabelsFromTags(tags map[string]string) map[string]any {
	l := make(map[string]any, len(tags))
	for key, value := range tags {
		l[key] = value
	}

	return l
}

func LabelSelector(l map[string]string) string {
	sb := strings.Builder{}
	for k, v := range l {
		// prevents trailing comma at the end
		if sb.Len() > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "%s=%s", k, v)
	}
	return sb.String()
}

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
