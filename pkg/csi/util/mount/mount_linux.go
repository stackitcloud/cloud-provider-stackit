//go:build linux

package mount

import (
	"path/filepath"

	"golang.org/x/sys/unix"
)

const (
	pciDevicesPath = "/sys/bus/pci/devices"
	kubeletDir     = "/var/lib/kubelet"
)

func newDeviceStats(statfs *unix.Statfs_t) *DeviceStats {
	return &DeviceStats{
		Block: false,

		AvailableBytes: int64(statfs.Bavail) * statfs.Bsize,
		TotalBytes:     int64(statfs.Blocks) * statfs.Bsize,
		UsedBytes:      (int64(statfs.Blocks) - int64(statfs.Bfree)) * statfs.Bsize,

		AvailableInodes: int64(statfs.Ffree),
		TotalInodes:     int64(statfs.Files),
		UsedInodes:      int64(statfs.Files) - int64(statfs.Ffree),
	}
}

// CountFreePCIeSlots returns the number of PCIe root ports that are not occupied.
func CountFreePCIeSlots() (int64, error) {
	return countFreePCIeSlotsAt(pciDevicesPath)
}

// CountLocalCSIVolumes counts staged CSI volumes for the given driver.
func CountLocalCSIVolumes(driverName string) (int64, error) {
	csiPluginDir := filepath.Join(kubeletDir, "plugins", "kubernetes.io", "csi")
	return countLocalCSIVolumesAt(csiPluginDir, driverName)
}
