package kubetest2

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("extractServiceAccountEmail", func() {
	It("extracts the email from the credentials.iss field", func() {
		email, err := extractServiceAccountEmail(validServiceAccountKey)
		Expect(err).NotTo(HaveOccurred())
		Expect(email).To(Equal("owner@example.com"))
	})

	DescribeTable("rejects invalid service account keys",
		func(key string, wantErrContains string) {
			_, err := extractServiceAccountEmail(key)
			Expect(err).To(MatchError(ContainSubstring(wantErrContains)))
		},
		Entry("malformed JSON", "not-json", "parse service account key"),
		Entry("missing credentials.iss", `{"credentials":{"aud":"x"}}`, "no email in credentials.iss"),
		Entry("blank credentials.iss", `{"credentials":{"iss":"   "}}`, "no email in credentials.iss"),
	)
})
