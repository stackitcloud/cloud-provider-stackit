package blockstorage

import "testing"

func TestDriverScopedKeys(t *testing.T) {
	originalDriverName := DriverName
	t.Cleanup(func() {
		DriverName = originalDriverName
	})

	DriverName = "kubetest2.csi.stackit.cloud"

	if got := activeDriverName(false); got != "kubetest2.csi.stackit.cloud" {
		t.Fatalf("activeDriverName(false) = %q, want %q", got, "kubetest2.csi.stackit.cloud")
	}

	if got := activeDriverName(true); got != legacyDriverName {
		t.Fatalf("activeDriverName(true) = %q, want %q", got, legacyDriverName)
	}

	if got := activeTopologyKey(false); got != "topology.kubetest2.csi.stackit.cloud/zone" {
		t.Fatalf("activeTopologyKey(false) = %q", got)
	}

	if got := activeTopologyKey(true); got != "topology.cinder.csi.openstack.org/zone" {
		t.Fatalf("activeTopologyKey(true) = %q", got)
	}

	if got := driverResizeRequiredKey(); got != "kubetest2.csi.stackit.cloud/resizeRequired" {
		t.Fatalf("driverResizeRequiredKey() = %q", got)
	}

	if got := driverClusterIDKey(); got != "kubetest2.csi.stackit.cloud/cluster" {
		t.Fatalf("driverClusterIDKey() = %q", got)
	}
}
