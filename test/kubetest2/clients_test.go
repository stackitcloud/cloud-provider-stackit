package kubetest2

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("apiClientOptions", func() {
	It("configures the service account key when provided", func() {
		opts := apiClientOptions(validServiceAccountKey, "", "https://api.example.com")

		Expect(opts).To(HaveLen(2))
	})

	It("configures workload identity federation when only an email is provided", func() {
		opts := apiClientOptions("", "owner@example.com", "https://api.example.com")

		Expect(opts).To(HaveLen(3))
	})

	It("configures neither auth when neither key nor email is provided", func() {
		opts := apiClientOptions("", "", "https://api.example.com")

		Expect(opts).To(HaveLen(1))
	})

	It("configures no options when everything is empty", func() {
		Expect(apiClientOptions("", "", "")).To(BeEmpty())
	})
})
