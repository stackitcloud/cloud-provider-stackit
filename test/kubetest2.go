package main

import (
	"sigs.k8s.io/kubetest2/pkg/app"

	kubetest2stackit "github.com/stackitcloud/cloud-provider-stackit/test/kubetest2"
)

func main() {
	app.Main(kubetest2stackit.Name, kubetest2stackit.New)
}
