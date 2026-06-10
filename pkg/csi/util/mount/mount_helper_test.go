package mount

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCountFreePCIeSlotsAtMissingRoot(t *testing.T) {
	t.Parallel()

	_, err := countFreePCIeSlotsAt(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("countFreePCIeSlotsAt() error = nil, want error")
	}
}

func TestCountFreePCIeSlotsAtCountsOnlyFreeBridgeSlots(t *testing.T) {
	t.Parallel()

	devicesPath := t.TempDir()

	createPCIDevice(t, devicesPath, "0000:00:00.0", "0x060400")
	createPCIDevice(t, devicesPath, "0000:00:01.0", "0x060400", "0000:01:00.0")
	createPCIDevice(t, devicesPath, "0000:00:02.0", "0x010000", "0000:02:00.0")

	count, err := countFreePCIeSlotsAt(devicesPath)
	if err != nil {
		t.Fatalf("countFreePCIeSlotsAt() error = %v", err)
	}

	if count != 1 {
		t.Fatalf("countFreePCIeSlotsAt() = %d, want 1", count)
	}
}

func TestCountFreePCIeSlotsAtSkipsDevicesWithoutClass(t *testing.T) {
	t.Parallel()

	devicesPath := t.TempDir()

	createPCIDevice(t, devicesPath, "0000:00:00.0", "0x060400")
	mustMkdirAll(t, filepath.Join(devicesPath, "0000:00:01.0"))

	count, err := countFreePCIeSlotsAt(devicesPath)
	if err != nil {
		t.Fatalf("countFreePCIeSlotsAt() error = %v", err)
	}

	if count != 1 {
		t.Fatalf("countFreePCIeSlotsAt() = %d, want 1", count)
	}
}

func TestCountFreePCIeSlotsAtIgnoresNonPCIChildren(t *testing.T) {
	t.Parallel()

	devicesPath := t.TempDir()
	devPath := filepath.Join(devicesPath, "0000:00:00.0")
	mustMkdirAll(t, devPath)
	mustWriteFile(t, filepath.Join(devPath, "class"), "0x060400")
	mustMkdirAll(t, filepath.Join(devPath, "driver"))
	mustMkdirAll(t, filepath.Join(devPath, "not-a-pci-child"))

	count, err := countFreePCIeSlotsAt(devicesPath)
	if err != nil {
		t.Fatalf("countFreePCIeSlotsAt() error = %v", err)
	}

	if count != 1 {
		t.Fatalf("countFreePCIeSlotsAt() = %d, want 1", count)
	}
}

func TestCountLocalCSIVolumesAtMissingDir(t *testing.T) {
	t.Parallel()

	count, err := countLocalCSIVolumesAt(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("countLocalCSIVolumesAt() error = %v", err)
	}

	if count != 0 {
		t.Fatalf("countLocalCSIVolumesAt() = %d, want 0", count)
	}
}

func TestCountLocalCSIVolumesAtCountsOnlyGlobalMountDirs(t *testing.T) {
	t.Parallel()

	driverPluginDir := t.TempDir()

	mustMkdirAll(t, filepath.Join(driverPluginDir, "volume-a", globalMountDir))
	mustMkdirAll(t, filepath.Join(driverPluginDir, "volume-b", globalMountDir))
	mustMkdirAll(t, filepath.Join(driverPluginDir, "volume-c", "not-a-globalmount"))

	count, err := countLocalCSIVolumesAt(driverPluginDir)
	if err != nil {
		t.Fatalf("countLocalCSIVolumesAt() error = %v", err)
	}

	if count != 2 {
		t.Fatalf("countLocalCSIVolumesAt() = %d, want 2", count)
	}
}

func TestCountLocalCSIVolumesAtEmptyDir(t *testing.T) {
	t.Parallel()

	count, err := countLocalCSIVolumesAt(t.TempDir())
	if err != nil {
		t.Fatalf("countLocalCSIVolumesAt() error = %v", err)
	}

	if count != 0 {
		t.Fatalf("countLocalCSIVolumesAt() = %d, want 0", count)
	}
}

func TestCountLocalCSIVolumesAtReturnsZeroWhenDriverPathIsFile(t *testing.T) {
	t.Parallel()

	driverPluginDir := filepath.Join(t.TempDir(), "driver")
	mustWriteFile(t, driverPluginDir, "not a directory")

	count, err := countLocalCSIVolumesAt(driverPluginDir)
	if err != nil {
		t.Fatalf("countLocalCSIVolumesAt() error = %v", err)
	}

	if count != 0 {
		t.Fatalf("countLocalCSIVolumesAt() = %d, want 0", count)
	}
}

func createPCIDevice(t *testing.T, rootPath, deviceName, class string, children ...string) {
	t.Helper()

	devPath := filepath.Join(rootPath, deviceName)
	mustMkdirAll(t, devPath)
	mustWriteFile(t, filepath.Join(devPath, "class"), class)

	for _, child := range children {
		mustMkdirAll(t, filepath.Join(devPath, child))
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
