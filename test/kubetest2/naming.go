package kubetest2

import (
	"crypto/sha256"
	"encoding/hex"
)

func runTokenForRun(runID string) string {
	sum := sha256.Sum256([]byte(runID))
	return hex.EncodeToString(sum[:4])
}

func (d *Deployer) runToken() string {
	return runTokenForRun(d.options.RunID())
}

func (d *Deployer) clusterName() string {
	return "kt2" + d.runToken()
}

func (d *Deployer) projectName() string {
	return "kt2-" + d.runToken()
}

func (d *Deployer) serviceAccountName() string {
	return "kt2-" + d.runToken()
}
