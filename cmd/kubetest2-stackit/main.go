package main

import (
	"github.com/stackitcloud/cloud-provider-stackit/test/e2e/kubetest2-stackit/deployer"
	"sigs.k8s.io/kubetest2/pkg/app"
)

func main() {
	app.Main(deployer.Name, deployer.New)
}
