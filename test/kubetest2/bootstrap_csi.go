package kubetest2

import (
	"context"

	"k8s.io/klog/v2"
)

// ensureCSI deploys the STACKIT CSI driver into the freshly provisioned
// cluster as the last bootstrap step. Not yet implemented; will install the
// CSI Helm chart here.
func (d *Deployer) ensureCSI(_ context.Context) error {
	klog.Infof("CSI deployment not yet implemented, skipping")
	return nil
}
