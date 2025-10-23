package stackit

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Labels", func() {
	Describe("labelsFromTags", func() {
		It("should convert tags to labels", func() {
			tags := map[string]string{
				"key1": "value1",
				"key2": "value2",
			}

			labels := labelsFromTags(tags)

			Expect(labels).To(HaveKeyWithValue("key1", "value1"))
			Expect(labels).To(HaveKeyWithValue("key2", "value2"))
		})

		It("should handle empty tags", func() {
			tags := map[string]string{}

			labels := labelsFromTags(tags)

			Expect(labels).To(BeEmpty())
		})

		It("should handle nil tags", func() {
			var tags map[string]string

			labels := labelsFromTags(tags)

			Expect(labels).To(BeEmpty())
		})
	})
})
