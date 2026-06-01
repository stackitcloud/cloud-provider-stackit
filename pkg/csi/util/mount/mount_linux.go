//go:build linux

package mount

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"
)

var (
	pciAddressRegex = regexp.MustCompile(`^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-9a-fA-F]$`)
)

const (
	// PCIClassBridgePCI Linux constant: https://github.com/torvalds/linux/blob/e43ffb69e0438cddd72aaa30898b4dc446f664f8/include/linux/pci_ids.h#L62
	PCIClassBridgePCI = "0x0604"
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

// CountFreePCIeSlots returns the number of PCIe Root ports who
// are currently not occupied by anything.
func CountFreePCIeSlots() (int64, error) {
	const pciPath = "/sys/bus/pci/devices"

	// Get all PCI devices
	devices, err := os.ReadDir(pciPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read PCI bus: %w", err)
	}

	freePCIeSlots := 0

	for _, dev := range devices {
		devPath := filepath.Join(pciPath, dev.Name())

		// 1. Identify if it's a Root Port / Bridge
		// We check the 'class' file. PCI Bridge class code starts with 0x0604
		classBuf, err := os.ReadFile(filepath.Join(devPath, "class"))
		if err != nil {
			klog.Errorf("failed to read PCI device class %s : %v", devPath, err)
			continue
		}
		class := strings.TrimSpace(string(classBuf))

		// Class 0x060400 is a PCI-to-PCI bridge (standard for Root Ports)
		if strings.HasPrefix(class, PCIClassBridgePCI) {
			// 2. Check if the port has downstream devices
			// If the bridge has children, they appear as subdirectories
			// matching the PCI address format (e.g., 0000:01:00.0)
			files, err2 := os.ReadDir(devPath)
			if err2 != nil {
				klog.Errorf("failed to read dir %s : %v", devPath, err2)
			}
			hasDownStreamFolder := slices.ContainsFunc(files, func(s os.DirEntry) bool {
				return pciAddressRegex.MatchString(s.Name())
			})
			if !hasDownStreamFolder {
				freePCIeSlots += 1
			}
		} else {
			klog.V(4).Infof("skipping class %s: path: %s", class, devPath)
		}
	}

	return int64(freePCIeSlots), nil
}

// CountLocalCSIVolumes tries to count how many volumes are mounted for a given driverName.
func CountLocalCSIVolumes(driverName string) (int64, error) {
	const kubeletDir = "/var/lib/kubelet"
	volumeCount := 0
	// The path where Kubelet mounts global tracking directories for a specific CSI driver
	targetDir := filepath.Join(kubeletDir, "plugins", "kubernetes.io", "csi", driverName)

	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, fmt.Errorf("failed to check directory: %w", err)
	}

	volumes, err := os.ReadDir(targetDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read dir %s: %w", targetDir, err)
	}
	for _, vol := range volumes {
		// Check if volume has a "globalmount" dir to determine if it's mounted correctly
		globalMountPath := filepath.Join(vol.Name(), "globalmount")
		if _, err := os.Stat(globalMountPath); os.IsNotExist(err) {
			continue
		}

		volumeCount++
	}

	return int64(volumeCount), nil
}
