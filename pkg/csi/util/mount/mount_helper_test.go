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
			count, err := countLocalCSIVolumesAt(filepath.Join(GinkgoT().TempDir(), "missing"))
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(BeZero())
		})

		It("counts only global mount directories", func() {
			driverPluginDir := GinkgoT().TempDir()

			mustMkdirAll(filepath.Join(driverPluginDir, "volume-a", globalMountDir))
			mustMkdirAll(filepath.Join(driverPluginDir, "volume-b", globalMountDir))
			mustMkdirAll(filepath.Join(driverPluginDir, "volume-c", "not-a-globalmount"))

			count, err := countLocalCSIVolumesAt(driverPluginDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(2)))
		})

		It("returns zero for an empty driver directory", func() {
			count, err := countLocalCSIVolumesAt(GinkgoT().TempDir())
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(BeZero())
		})

		It("returns zero when the driver path is a file", func() {
			driverPluginDir := filepath.Join(GinkgoT().TempDir(), "driver")
			mustWriteFile(driverPluginDir, "not a directory")

			count, err := countLocalCSIVolumesAt(driverPluginDir)
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
	Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
}
