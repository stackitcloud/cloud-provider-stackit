package kubetest2

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestKubetest2(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kubetest2 Suite")
}
