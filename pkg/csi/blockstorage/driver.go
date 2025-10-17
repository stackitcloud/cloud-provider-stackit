package blockstorage

import (
	"fmt"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit"
	corev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/csi/util/mount"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/metadata"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/version"
)

const (
	driverName  = "block-storage.csi.stackit.cloud"
	topologyKey = "topology." + driverName + "/zone"

	// ResizeRequired parameter, if set to true, will trigger a resize on mount operation
	ResizeRequired = driverName + "/resizeRequired"
)

var (
	// CSI spec version
	specVersion = "1.12.0"
	Version     = "1.0.0"
)

type Driver struct {
	name         string
	fqVersion    string // Fully qualified version in format {Version}@{CPO version}
	endpoint     string
	clusterID    string
	withTopology bool

	ids *identityServer
	cs  *controllerServer
	ns  *nodeServer

	vcap  []*csi.VolumeCapability_AccessMode
	cscap []*csi.ControllerServiceCapability
	nscap []*csi.NodeServiceCapability
	csi.UnimplementedNodeServer

	pvcLister corev1.PersistentVolumeClaimLister
}

type DriverOpts struct {
	ClusterID    string
	Endpoint     string
	WithTopology bool

	PVCLister corev1.PersistentVolumeClaimLister
}

func NewDriver(o *DriverOpts) *Driver {
	d := &Driver{
		name:         driverName,
		fqVersion:    fmt.Sprintf("%s@%s", Version, version.Version),
		endpoint:     o.Endpoint,
		clusterID:    o.ClusterID,
		withTopology: o.WithTopology,
		pvcLister:    o.PVCLister,
	}

	klog.Info("Driver: ", d.name)
	klog.Info("Driver version: ", d.fqVersion)
	klog.Info("CSI Spec version: ", specVersion)
	klog.Infof("Topology awareness: %t", d.withTopology)

	d.AddControllerServiceCapabilities(
		[]csi.ControllerServiceCapability_RPC_Type{
			csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
			csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
			csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
			csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
			csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
			csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
			csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
			csi.ControllerServiceCapability_RPC_LIST_VOLUMES_PUBLISHED_NODES,
			csi.ControllerServiceCapability_RPC_GET_VOLUME,
		})
	d.AddVolumeCapabilityAccessModes(
		[]csi.VolumeCapability_AccessMode_Mode{
			csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		})

	// ignoring error, because AddNodeServiceCapabilities is public
	// and so potentially used somewhere else.
	_ = d.AddNodeServiceCapabilities(
		[]csi.NodeServiceCapability_RPC_Type{
			csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
			csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
			csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
		})

	d.ids = NewIdentityServer(d)

	return d
}

func (d *Driver) AddControllerServiceCapabilities(cl []csi.ControllerServiceCapability_RPC_Type) {
	csc := make([]*csi.ControllerServiceCapability, 0, len(cl))

	for _, c := range cl {
		klog.Infof("Enabling controller service capability: %v", c.String())
		csc = append(csc, NewControllerServiceCapability(c))
	}

	d.cscap = csc
}

func (d *Driver) AddVolumeCapabilityAccessModes(vc []csi.VolumeCapability_AccessMode_Mode) []*csi.VolumeCapability_AccessMode {
	vca := make([]*csi.VolumeCapability_AccessMode, 0, len(vc))

	for _, c := range vc {
		klog.Infof("Enabling volume access mode: %v", c.String())
		vca = append(vca, NewVolumeCapabilityAccessMode(c))
	}

	d.vcap = vca

	return vca
}

func (d *Driver) AddNodeServiceCapabilities(nl []csi.NodeServiceCapability_RPC_Type) error {
	nsc := make([]*csi.NodeServiceCapability, 0, len(nl))

	for _, n := range nl {
		klog.Infof("Enabling node service capability: %v", n.String())
		nsc = append(nsc, NewNodeServiceCapability(n))
	}

	d.nscap = nsc

	return nil
}

func (d *Driver) SetupControllerService(instance stackit.IaasClient) {
	klog.Info("Providing controller service")
	d.cs = NewControllerServer(d, instance)
}

func (d *Driver) SetupNodeService(mountProvider mount.IMount, metadataProvider metadata.IMetadata, opts stackit.BlockStorageOpts, topologies map[string]string) {
	klog.Info("Providing node service")
	d.ns = NewNodeServer(d, mountProvider, metadataProvider, opts, topologies)
}

func (d *Driver) Run() {
	if d.cs == nil && d.ns == nil {
		klog.Fatal("No CSI services initialized")
	}

	RunServicesInitialized(d.endpoint, d.ids, d.cs, d.ns)
}
