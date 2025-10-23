package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RoundUpSize", func() {
	Context("when calculating allocation units", func() {
		It("should return exact multiple when volume size is exact", func() {
			// 10GB volume with 1GB allocation units
			result := RoundUpSize(10*GIBIBYTE, 1*GIBIBYTE)
			Expect(result).To(Equal(int64(10)))
		})

		It("should round up when volume size is not exact", func() {
			// 1500MB volume with 1GB allocation units
			result := RoundUpSize(1500*MEBIBYTE, 1*GIBIBYTE)
			Expect(result).To(Equal(int64(2)))
		})

		It("should handle zero volume size", func() {
			result := RoundUpSize(0, 1*GIBIBYTE)
			Expect(result).To(Equal(int64(0)))
		})

		It("should handle allocation unit larger than volume", func() {
			// 500MB volume with 1GB allocation units
			result := RoundUpSize(500*MEBIBYTE, 1*GIBIBYTE)
			Expect(result).To(Equal(int64(1)))
		})
	})
})
