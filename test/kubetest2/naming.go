package kubetest2

import (
	"crypto/sha256"
	"encoding/hex"
)

func clusterNameForRun(runID string) string {
	sum := sha256.Sum256([]byte(runID))
	return "kt2" + hex.EncodeToString(sum[:4])
}

func runTokenForRun(runID string) string {
	sum := sha256.Sum256([]byte(runID))
	return hex.EncodeToString(sum[:4])
}

func projectNameForRun(runID string) string {
	return "kt2-" + runTokenForRun(runID)
}

func serviceAccountNameForRun(runID string) string {
	return "kt2-" + runTokenForRun(runID)
}

func (d *Deployer) clusterName() string {
	return clusterNameForRun(d.options.RunID())
}

func (d *Deployer) runToken() string {
	return runTokenForRun(d.options.RunID())
}

func (d *Deployer) projectName() string {
	return projectNameForRun(d.options.RunID())
}

func (d *Deployer) serviceAccountName() string {
	return serviceAccountNameForRun(d.options.RunID())
}
