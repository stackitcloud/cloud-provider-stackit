package deployassets

import "embed"

// FS contains the vendored install assets used by the kubetest2 STACKIT deployer.
//
//go:embed csi-plugin/*.yaml snapshot-controller/*.yaml snapshot-controller/crds/*.yaml
var FS embed.FS
