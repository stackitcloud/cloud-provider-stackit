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
	Context("when validating label keys", func() {
		It("should be valid with a standard key", func() {
			result := IsValidLabelKey("my-label-key")
			Expect(result).To(BeTrue())
		})

		It("should be valid with dot and underscore", func() {
			result := IsValidLabelKey("my.label_key")
			Expect(result).To(BeTrue())
		})

		It("should be valid with numbers", func() {
			result := IsValidLabelKey("my-label-key-123")
			Expect(result).To(BeTrue())
		})

		It("should be valid with a single character", func() {
			result := IsValidLabelKey("a")
			Expect(result).To(BeTrue())
		})

		It("should be invalid when starting with a hyphen", func() {
			result := IsValidLabelKey("-my-label-key")
			Expect(result).To(BeFalse())
		})

		It("should be invalid when ending with a hyphen", func() {
			result := IsValidLabelKey("my-label-key-")
			Expect(result).To(BeFalse())
		})

		It("should be invalid when starting with a dot", func() {
			result := IsValidLabelKey(".my-label-key")
			Expect(result).To(BeFalse())
		})

		It("should be invalid when starting with an underscore", func() {
			result := IsValidLabelKey("_my-label-key")
			Expect(result).To(BeFalse())
		})

		It("should be invalid when ending with a dot", func() {
			result := IsValidLabelKey("my-label-key.")
			Expect(result).To(BeFalse())
		})

		It("should be invalid when ending with an underscore", func() {
			result := IsValidLabelKey("my-label-key_")
			Expect(result).To(BeFalse())
		})

		It("should be invalid when empty", func() {
			result := IsValidLabelKey("")
			Expect(result).To(BeFalse())
		})

		It("should be invalid when the key is too long", func() {
			result := IsValidLabelKey("this-is-a-very-long-label-key-that-exceeds-the-maximum-length-of-63-characters-and-should-be-invalid")
			Expect(result).To(BeFalse())
		})
	})

	Context("when validating label values", func() {
		It("should be valid with a standard value", func() {
			result := IsValidLabelValue("my-label-value")
			Expect(result).To(BeTrue())
		})

		It("should be valid with dot and underscore", func() {
			result := IsValidLabelValue("my.label_value")
			Expect(result).To(BeTrue())
		})

		It("should be valid with numbers", func() {
			result := IsValidLabelValue("my-label-value-123")
			Expect(result).To(BeTrue())
		})

		It("should be valid when empty", func() {
			result := IsValidLabelValue("")
			Expect(result).To(BeTrue())
		})

		It("should be invalid when starting with a hyphen", func() {
			result := IsValidLabelValue("-my-label-value")
			Expect(result).To(BeFalse())
		})

		It("should be invalid when ending with a hyphen", func() {
			result := IsValidLabelValue("my-label-value-")
			Expect(result).To(BeFalse())
		})

		It("should be invalid when starting with a dot", func() {
			result := IsValidLabelValue(".my-label-value")
			Expect(result).To(BeFalse())
		})

		It("should be invalid when starting with an underscore", func() {
			result := IsValidLabelValue("_my-label-value")
			Expect(result).To(BeFalse())
		})

		It("should be invalid when ending with a dot", func() {
			result := IsValidLabelValue("my-label-value.")
			Expect(result).To(BeFalse())
		})

		It("should be invalid when ending with an underscore", func() {
			result := IsValidLabelValue("my-label-value_")
			Expect(result).To(BeFalse())
		})

		It("should be invalid when the value is too long", func() {
			result := IsValidLabelValue("this-is-a-very-long-label-value-that-exceeds-the-maximum-length-of-63-characters-and-should-be-invalid")
			Expect(result).To(BeFalse())
		})
	})
})
