package blockstorage

import (
	"context"
	"strings"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

func TestValidateDriverOptsRejectsLegacyWithCustomDriverName(t *testing.T) {
	err := ValidateDriverOpts(&DriverOpts{
		DriverName:       "custom.csi.stackit.cloud",
		LegacyDriverName: true,
	})
	if err == nil || !strings.Contains(err.Error(), "--legacy-storage-mode") {
		t.Fatalf("expected legacy/custom conflict error, got %v", err)
	}
}

func TestNewDriverUsesConfiguredDriverName(t *testing.T) {
	driver := NewDriver(&DriverOpts{
		Endpoint:   "unix://tmp/csi.sock",
		ClusterID:  "cluster-id",
		DriverName: "custom.csi.stackit.cloud",
	})

	info, err := driver.ids.GetPluginInfo(context.Background(), &csi.GetPluginInfoRequest{})
	if err != nil {
		t.Fatalf("GetPluginInfo() returned error: %v", err)
	}
	if info.Name != "custom.csi.stackit.cloud" {
		t.Fatalf("unexpected driver name: %q", info.Name)
	}
	if driver.topologyKey() != "topology.custom.csi.stackit.cloud/zone" {
		t.Fatalf("unexpected topology key: %q", driver.topologyKey())
	}
	if driver.resizeRequiredKey() != "custom.csi.stackit.cloud/resizeRequired" {
		t.Fatalf("unexpected resize annotation key: %q", driver.resizeRequiredKey())
	}
	if driver.clusterMetadataKey() != "custom.csi.stackit.cloud/cluster" {
		t.Fatalf("unexpected cluster metadata key: %q", driver.clusterMetadataKey())
	}
}
