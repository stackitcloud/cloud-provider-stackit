package mount

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Mount helpers", func() {
	Describe("countFreePCIeSlotsAt", func() {
		It("returns an error when the PCI devices root is missing", func() {
			_, err := countFreePCIeSlotsAt(filepath.Join(GinkgoT().TempDir(), "missing"))
			Expect(err).To(HaveOccurred())
		})

		It("counts only free bridge-backed PCIe slots", func() {
			devicesPath := GinkgoT().TempDir()

			createPCIDevice(devicesPath, "0000:00:00.0", "0x060400")
			createPCIDevice(devicesPath, "0000:00:01.0", "0x060400", "0000:01:00.0")
			createPCIDevice(devicesPath, "0000:00:02.0", "0x010000", "0000:02:00.0")

			count, err := countFreePCIeSlotsAt(devicesPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(1)))
		})

		It("skips devices whose class cannot be read", func() {
			devicesPath := GinkgoT().TempDir()

			createPCIDevice(devicesPath, "0000:00:00.0", "0x060400")
			mustMkdirAll(filepath.Join(devicesPath, "0000:00:01.0"))

			count, err := countFreePCIeSlotsAt(devicesPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(1)))
		})

		It("ignores non-PCI child entries when checking bridge occupancy", func() {
			devicesPath := GinkgoT().TempDir()
			devPath := filepath.Join(devicesPath, "0000:00:00.0")
			mustMkdirAll(devPath)
			mustWriteFile(filepath.Join(devPath, "class"), "0x060400")
			mustMkdirAll(filepath.Join(devPath, "driver"))
			mustMkdirAll(filepath.Join(devPath, "not-a-pci-child"))

			count, err := countFreePCIeSlotsAt(devicesPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(1)))
		})
	})

	Describe("countLocalCSIVolumesAt", func() {
		It("returns zero for a missing driver directory", func() {
			tempDir := GinkgoT().TempDir()

			count, err := countLocalCSIVolumesAt(tempDir, "block-storage.csi.stackit.cloud")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(BeZero())
		})

		It("counts filesystem and block volumes together", func() {
			csiPluginDir := GinkgoT().TempDir()
			driverPluginDir := filepath.Join(csiPluginDir, "block-storage.csi.stackit.cloud")

			mustMkdirAll(filepath.Join(driverPluginDir, "volume-a", globalMountDir))
			mustMkdirAll(filepath.Join(driverPluginDir, "volume-b", globalMountDir))
			mustMkdirAll(filepath.Join(driverPluginDir, "volume-c", "not-a-globalmount"))
			mustWriteVolumeMetadata(csiPluginDir, "block-volume-a", "block-storage.csi.stackit.cloud")
			mustWriteVolumeMetadata(csiPluginDir, "block-volume-b", "block-storage.csi.stackit.cloud")
			mustWriteVolumeMetadata(csiPluginDir, "other-driver-volume", "other.csi.example")

			count, err := countLocalCSIVolumesAt(csiPluginDir, "block-storage.csi.stackit.cloud")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(4)))
		})

		It("returns zero for an empty driver directory", func() {
			csiPluginDir := GinkgoT().TempDir()

			count, err := countLocalCSIVolumesAt(csiPluginDir, "block-storage.csi.stackit.cloud")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(BeZero())
		})

		It("returns zero when the computed driver path is a file", func() {
			csiPluginDir := GinkgoT().TempDir()
			driverPluginDir := filepath.Join(csiPluginDir, "block-storage.csi.stackit.cloud")
			mustWriteFile(driverPluginDir, "not a directory")

			count, err := countLocalCSIVolumesAt(csiPluginDir, "block-storage.csi.stackit.cloud")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(BeZero())
		})

		It("still counts filesystem volumes when volumeDevices is missing", func() {
			csiPluginDir := GinkgoT().TempDir()
			driverPluginDir := filepath.Join(csiPluginDir, "block-storage.csi.stackit.cloud")
			mustMkdirAll(filepath.Join(driverPluginDir, "volume-a", globalMountDir))

			count, err := countLocalCSIVolumesAt(csiPluginDir, "block-storage.csi.stackit.cloud")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(1)))
		})

		It("ignores block metadata for other CSI drivers", func() {
			csiPluginDir := GinkgoT().TempDir()
			mustWriteVolumeMetadata(csiPluginDir, "block-volume-a", "other.csi.example")

			count, err := countLocalCSIVolumesAt(csiPluginDir, "block-storage.csi.stackit.cloud")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(BeZero())
		})

		It("counts volumes for a non-default CSI driver name", func() {
			csiPluginDir := GinkgoT().TempDir()
			driverName := "other.csi.example"
			driverPluginDir := filepath.Join(csiPluginDir, driverName)

			mustMkdirAll(filepath.Join(driverPluginDir, "volume-a", globalMountDir))
			mustWriteVolumeMetadata(csiPluginDir, "block-volume-a", driverName)
			mustWriteVolumeMetadata(csiPluginDir, "block-volume-b", "block-storage.csi.stackit.cloud")

			count, err := countLocalCSIVolumesAt(csiPluginDir, driverName)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(2)))
		})

		It("skips malformed or unreadable block metadata files", func() {
			csiPluginDir := GinkgoT().TempDir()
			mustWriteVolumeMetadata(csiPluginDir, "good-volume", "block-storage.csi.stackit.cloud")
			malformedPath := filepath.Join(csiPluginDir, volumeDevicesDir, "malformed-volume", volumeDataDir, volumeDataFile)
			mustMkdirAll(filepath.Dir(malformedPath))
			mustWriteFile(malformedPath, "{not-json")
			unreadablePath := filepath.Join(csiPluginDir, volumeDevicesDir, "unreadable-volume", volumeDataDir, volumeDataFile)
			mustMkdirAll(filepath.Dir(unreadablePath))
			mustWriteFile(unreadablePath, `{"driverName":"block-storage.csi.stackit.cloud"}`)
			Expect(os.Chmod(unreadablePath, 0o000)).To(Succeed())
			defer func() {
				Expect(os.Chmod(unreadablePath, 0o644)).To(Succeed())
			}()

			count, err := countLocalCSIVolumesAt(csiPluginDir, "block-storage.csi.stackit.cloud")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(1)))
		})

		It("does not count publish or staging directories without metadata", func() {
			csiPluginDir := GinkgoT().TempDir()
			mustMkdirAll(filepath.Join(csiPluginDir, volumeDevicesDir, "spec-a", "publish", "pod-a"))
			mustMkdirAll(filepath.Join(csiPluginDir, volumeDevicesDir, "spec-b", "staging"))

			count, err := countLocalCSIVolumesAt(csiPluginDir, "block-storage.csi.stackit.cloud")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(BeZero())
		})
	})

	Describe("countLocalCSIBlockVolumesAt", func() {
		It("returns zero when volumeDevices is missing", func() {
			count, err := countLocalCSIBlockVolumesAt(GinkgoT().TempDir(), "block-storage.csi.stackit.cloud")
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(BeZero())
		})
	})
})

func createPCIDevice(rootPath, deviceName, class string, children ...string) {
	GinkgoHelper()

	devPath := filepath.Join(rootPath, deviceName)
	mustMkdirAll(devPath)
	mustWriteFile(filepath.Join(devPath, "class"), class)

	for _, child := range children {
		mustMkdirAll(filepath.Join(devPath, child))
	}
}

func mustMkdirAll(path string) {
	GinkgoHelper()
	Expect(os.MkdirAll(path, 0o755)).To(Succeed())
}

func mustWriteFile(path, content string) {
	GinkgoHelper()
	mustMkdirAll(filepath.Dir(path))
	Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
}

func mustWriteVolumeMetadata(csiPluginDir, volumeName, driverName string) {
	GinkgoHelper()
	mustWriteFile(
		filepath.Join(csiPluginDir, volumeDevicesDir, volumeName, volumeDataDir, volumeDataFile),
		`{"driverName":"`+driverName+`"}`,
	)
}
