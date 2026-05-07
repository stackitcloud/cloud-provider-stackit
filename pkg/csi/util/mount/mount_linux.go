//go:build linux

package mount

import "golang.org/x/sys/unix"

var (
	pciAddressRegex = regexp.MustCompile(`^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-9a-fA-F]$`)
)

const (
	RedhatVendor      = "0x1af4"
	VirtioBlockDevice = "0x1042"
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

// CountNonVirtioBlockDevices returns the number of PCIe Root ports who
// are currently occupied by anything else than an VIRTIO 1.0 Block Device
// returns zero when something went wrong
func CountNonVirtioBlockDevices() (int64, error) {
	const pciPath = "/sys/bus/pci/devices"

	// Get all PCI devices
	devices, err := os.ReadDir(pciPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read PCI bus: %w", err)
	}

	pcieSlotsOccupiedByNonBlockDevice := 0

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
		if strings.HasPrefix(class, "0x0604") {
			// 2. Check if the port has downstream devices
			// If the bridge has children, they appear as subdirectories
			// matching the PCI address format (e.g., 0000:01:00.0)
			files, err2 := os.ReadDir(devPath)
			if err2 != nil {
				klog.Errorf("failed to read dir %s : %v", devPath, err2)
			}
			for _, file := range files {
				// Ignore PCI bus directories such as pci001 pci002 and pci010
				// Devices must follow <domain:bus:device.function> format
				if pciAddressRegex.MatchString(file.Name()) {
					isNonBlockDevice := IsNonBlockDevice(devPath, file)
					if isNonBlockDevice {
						pcieSlotsOccupiedByNonBlockDevice++
					}
					break
				}
			}
		} else {
			klog.V(4).Infof("skipping class %s: path: %s", class, devPath)
		}
	}

	return int64(pcieSlotsOccupiedByNonBlockDevice), nil
}

func IsNonBlockDevice(devPath string, file os.DirEntry) bool {
	var isNonBlockDevice bool
	pciDevicePath := filepath.Join(devPath, file.Name())
	vendorBuf, err := os.ReadFile(filepath.Join(pciDevicePath, "vendor"))
	if err != nil {
		klog.Errorf("failed to read PCI device vendor %s : %v", pciDevicePath, err)
	}
	deviceBuf, err := os.ReadFile(filepath.Join(pciDevicePath, "device"))
	if err != nil {
		klog.Errorf("failed to read PCI device file %s : %v", pciDevicePath, err)
	}
	if strings.TrimSpace(string(vendorBuf)) == RedhatVendor && strings.TrimSpace(string(deviceBuf)) != VirtioBlockDevice {
		isNonBlockDevice = true
	}
	return isNonBlockDevice
}
