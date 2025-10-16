package stackit

type VolumeSourceTypes string

const (
	VolumeSource   VolumeSourceTypes = "volume"
	SnapshotSource VolumeSourceTypes = "snapshot"
	BackupSource   VolumeSourceTypes = "backup"
)
