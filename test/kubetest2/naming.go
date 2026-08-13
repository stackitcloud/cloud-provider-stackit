package kubetest2

import (
	"crypto/sha256"
	"encoding/hex"
)

func clusterNameForRun(runID string) string {
	sum := sha256.Sum256([]byte(runID))
	return "kt2" + hex.EncodeToString(sum[:4])
}

func (d *Deployer) clusterName() string {
	return clusterNameForRun(d.options.RunID())
}
