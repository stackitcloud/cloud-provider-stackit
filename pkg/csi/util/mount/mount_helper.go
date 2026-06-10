package mount

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/klog/v2"
)

const (
	// pciClassBridgePCI matches the Linux PCI-to-PCI bridge class prefix.
	pciClassBridgePCI = "0x0604"
	globalMountDir    = "globalmount"
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

		class := strings.TrimSpace(string(classBuf))
		if !strings.HasPrefix(class, pciClassBridgePCI) {
			continue
		}

		children, err := filepath.Glob(filepath.Join(devPath, "????:??:??.?"))
		if err != nil {
			return 0, fmt.Errorf("failed to glob PCI children for %s: %w", devPath, err)
		}

		if len(children) == 0 {
			freePCIeSlots++
		}
	}

	return freePCIeSlots, nil
}

func countLocalCSIVolumesAt(driverPluginDir string) (int64, error) {
	volumeMounts, err := filepath.Glob(filepath.Join(driverPluginDir, "*", globalMountDir))
	if err != nil {
		return 0, fmt.Errorf("failed to glob CSI volume mounts in %s: %w", driverPluginDir, err)
	}

	return int64(len(volumeMounts)), nil
}
