package labels

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Sanitize", func() {
	Context("when sanitizing labels", func() {
		It("should replace non-alphanumeric characters with hyphens", func() {
			result := Sanitize("test-label_with.special@chars!")
			Expect(result).To(Equal("test-label_with.special-chars"))
		})

		It("should trim hyphens, underscores, and dots from the beginning and end", func() {
			result := Sanitize("...test-label---")
			Expect(result).To(Equal("test-label"))
		})

		It("should truncate labels longer than 63 characters", func() {
			longLabel := "this-is-a-very-long-label-that-should-be-truncated-to-63-characters-1234567890"
			result := Sanitize(longLabel)
			Expect(len(result)).To(BeNumerically("<=", 63))
			Expect(result).To(Equal("this-is-a-very-long-label-that-should-be-truncated-to-63-charac"))
		})

		It("should handle empty string", func() {
			result := Sanitize("")
			Expect(result).To(Equal(""))
		})

		It("should handle string with only invalid characters", func() {
			result := Sanitize("!@#$%^&*()")
			Expect(result).To(Equal(""))
		})
	})
})
