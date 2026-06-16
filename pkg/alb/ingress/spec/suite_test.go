package spec

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestStackit(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ALB Spec")
}
