package blockstorage

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/stackitcloud/cloud-provider-stackit/pkg/csi/util/mount"
	stackitclient "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/client"
	stackitconfig "github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/config"
	"github.com/stackitcloud/cloud-provider-stackit/pkg/stackit/metadata"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
)

var serverGRPCEndpointCallCounter atomic.Uint64

func NewControllerServiceCapability(rpcType csi.ControllerServiceCapability_RPC_Type) *csi.ControllerServiceCapability {
	return &csi.ControllerServiceCapability{
		Type: &csi.ControllerServiceCapability_Rpc{
			Rpc: &csi.ControllerServiceCapability_RPC{
				Type: rpcType,
			},
		},
	}
}

func NewNodeServiceCapability(rpcType csi.NodeServiceCapability_RPC_Type) *csi.NodeServiceCapability {
	return &csi.NodeServiceCapability{
		Type: &csi.NodeServiceCapability_Rpc{
			Rpc: &csi.NodeServiceCapability_RPC{
				Type: rpcType,
			},
		},
	}
}

func NewVolumeCapabilityAccessMode(mode csi.VolumeCapability_AccessMode_Mode) *csi.VolumeCapability_AccessMode {
	return &csi.VolumeCapability_AccessMode{Mode: mode}
}

//revive:disable:unexported-return
func NewControllerServer(d *Driver, instance stackitclient.IaaSClient) *controllerServer {
	return &controllerServer{
		Driver:   d,
		Instance: instance,
	}
}

func NewIdentityServer(d *Driver) *identityServer {
	return &identityServer{
		Driver: d,
	}
}

func NewNodeServer(d *Driver, mountProvider mount.IMount, metadataProvider metadata.IMetadata, opts stackitconfig.BlockStorageOpts) *nodeServer { //nolint:lll // looks weird when shortened
	return &nodeServer{
		Driver:   d,
		Mount:    mountProvider,
		Metadata: metadataProvider,
		Opts:     opts,
	}
}

//revive:enable:unexported-return

func RunServicesInitialized(endpoint string, ids csi.IdentityServer, cs csi.ControllerServer, ns csi.NodeServer) {
	s := NewNonBlockingGRPCServer()
	s.Start(endpoint, ids, cs, ns)
	s.Wait()
}

func ParseEndpoint(ep string) (proto, addr string, err error) {
	if strings.HasPrefix(strings.ToLower(ep), "unix://") || strings.HasPrefix(strings.ToLower(ep), "tcp://") {
		s := strings.SplitN(ep, "://", 2)
		if s[1] != "" {
			return s[0], s[1], nil
		}
	}
	return "", "", fmt.Errorf("invalid endpoint: %v", ep)
}

func logGRPC(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	callID := serverGRPCEndpointCallCounter.Add(1)

	klog.V(3).Infof("[ID:%d] GRPC call: %s", callID, info.FullMethod)
	klog.V(5).Infof("[ID:%d] GRPC request: %s", callID, protosanitizer.StripSecrets(req))
	resp, err := handler(ctx, req)
	if err != nil {
		klog.Errorf("[ID:%d] GRPC error: %v", callID, err)
	} else {
		klog.V(5).Infof("[ID:%d] GRPC response: %s", callID, protosanitizer.StripSecrets(resp))
	}

	return resp, err
}
