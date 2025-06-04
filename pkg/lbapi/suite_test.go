package lbapi

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestLBAPI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "LBAPI Suite")
}
