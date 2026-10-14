package blockstorage

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Driver scoped keys", func() {
	var originalDriverName string

	BeforeEach(func() {
		originalDriverName = DriverName
		DriverName = "kubetest2.csi.stackit.cloud"
	})

	AfterEach(func() {
		DriverName = originalDriverName
	})

	It("uses the active driver name and derived keys for the non-legacy driver", func() {
		Expect(activeDriverName(false)).To(Equal("kubetest2.csi.stackit.cloud"))
		Expect(activeDriverName(true)).To(Equal(legacyDriverName))
		Expect(activeTopologyKey(false)).To(Equal("topology.kubetest2.csi.stackit.cloud/zone"))
		Expect(activeTopologyKey(true)).To(Equal("topology.cinder.csi.openstack.org/zone"))
		Expect(driverResizeRequiredKey()).To(Equal("kubetest2.csi.stackit.cloud/resizeRequired"))
		Expect(driverClusterIDKey()).To(Equal("kubetest2.csi.stackit.cloud/cluster"))
	})
})
