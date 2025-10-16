package blockstorage

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Util Test", func() {

	Context("DetermineMaxVolumesByFlavor", func() {
		DescribeTable("should return the correct maximum volume count for different flavors", func(flavor string, expectedMaxVolumes int) {
			maxVolumes := DetermineMaxVolumesByFlavor(flavor)
			Expect(maxVolumes).To(Equal(int64(expectedMaxVolumes)))
		},
			Entry("Intel 3rd Gen", "c3i.2", 25),
			Entry("Intel 2rd Gen", "c2i.2", 25),
			Entry("Intel 1st Gen", "c1.2", 25),
			Entry("AMD 1st Gen without overprovisioning", "s1a.8d", 25),
			Entry("AMD 2nd Gen without overprovisioning", "s2a.8d", 159),
			Entry("Nvidia GPU", "n2.14d.g1", 10),
			Entry("Nvidia GPU", "n2.56d.g4", 10),
			Entry("ARM Gen1Link without CPU-overprovisioning ARM Gen1", "g1r.4d", 25),
		)
	})
})
