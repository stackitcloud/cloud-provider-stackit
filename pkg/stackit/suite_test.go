package stackit

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSTACKITProvider(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CSI STACKIT Provider Suite")
}
