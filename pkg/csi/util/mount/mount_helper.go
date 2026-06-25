package mount

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/klog/v2"
)

var (
	// pciClassBridgePCI matches the Linux PCI-to-PCI bridge class prefix.
	pciClassBridgePCI = []byte("0x0604")
)

const (
	globalMountDir   = "globalmount"
	volumeDevicesDir = "volumeDevices"
	volumeDataDir    = "data"
	volumeDataFile   = "vol_data.json"
	driverNameKey    = "driverName"
)

func countFreePCIeSlotsAt(devicesPath string) (int64, error) {
	devices, err := os.ReadDir(devicesPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read PCI bus: %w", err)
	}

	var freePCIeSlots int64

	for _, dev := range devices {
		devPath := filepath.Join(devicesPath, dev.Name())

		classBuf, err := os.ReadFile(filepath.Join(devPath, "class"))
		if err != nil {
			klog.Errorf("failed to read PCI device class %s: %v", devPath, err)
			continue
		}

		if !bytes.HasPrefix(bytes.TrimSpace(classBuf), pciClassBridgePCI) {
			continue
		}

		children, err := filepath.Glob(filepath.Join(devPath, "????:??:??.?"))
		if err != nil {
			klog.Errorf("failed to glob PCI children for %s: %v", devPath, err)
			continue
		}

		if len(children) == 0 {
			freePCIeSlots++
		}
	}

	return freePCIeSlots, nil
}

//nolint:unparam // driverName is always the same when running the linter on macOS but different on linux
func countLocalCSIVolumesAt(csiPluginDir, driverName string) (int64, error) {
	driverPluginDir := filepath.Join(csiPluginDir, driverName)

	filesystemVolumes, err := countLocalCSIFilesystemVolumesAt(driverPluginDir)
	if err != nil {
		return 0, err
	}

	blockVolumes, err := countLocalCSIBlockVolumesAt(csiPluginDir, driverName)
	if err != nil {
		return 0, err
	}

	return filesystemVolumes + blockVolumes, nil
}

func countLocalCSIFilesystemVolumesAt(driverPluginDir string) (int64, error) {
	volumeMounts, err := filepath.Glob(filepath.Join(driverPluginDir, "*", globalMountDir))
	if err != nil {
		return 0, fmt.Errorf("failed to glob CSI volume mounts in %s: %w", driverPluginDir, err)
	}

	return int64(len(volumeMounts)), nil
}

func countLocalCSIBlockVolumesAt(csiPluginDir, driverName string) (int64, error) {
	volumeMetadataFiles, err := filepath.Glob(filepath.Join(csiPluginDir, volumeDevicesDir, "*", volumeDataDir, volumeDataFile))
	if err != nil {
		return 0, fmt.Errorf("failed to glob CSI block volume metadata in %s: %w", csiPluginDir, err)
	}

	var blockVolumes int64

	for _, metadataPath := range volumeMetadataFiles {
		metadata, err := readCSIVolumeDeviceMetadata(metadataPath)
		if err != nil {
			klog.Errorf("failed to read CSI block volume metadata %s: %v", metadataPath, err)
			continue
		}

		if metadata[driverNameKey] == driverName {
			blockVolumes++
		}
	}

	return blockVolumes, nil
}

func readCSIVolumeDeviceMetadata(metadataPath string) (map[string]string, error) {
	metadataBuf, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var metadata map[string]string
	if err := json.Unmarshal(metadataBuf, &metadata); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	return metadata, nil
}
