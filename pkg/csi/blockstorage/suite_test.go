package blockstorage

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestControllerServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Block Storage CSI Suite")
}
