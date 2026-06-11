package mount

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMount(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Mount Suite")
}
