package kubetest2

import (
	"path/filepath"

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

var _ = Describe("loadEnvironment", func() {
	envVarKeys := []string{
		"STACKIT_SERVICE_ACCOUNT",
		"STACKIT_PARENT_CONTAINER_ID",
		"STACKIT_RESOURCE_MANAGER_ENDPOINT",
		"STACKIT_SERVICE_ACCOUNT_ENDPOINT",
		"STACKIT_AUTHORIZATION_ENDPOINT",
		"STACKIT_SERVICE_ENABLEMENT_ENDPOINT",
		"STACKIT_SKE_ENDPOINT",
	}

	DescribeTable("validates required environment variables",
		func(env map[string]string, wantErrContains string) {
			for _, key := range envVarKeys {
				setEnvVar(key, "")
			}
			for key, value := range env {
				setEnvVar(key, value)
			}

			runDir := GinkgoT().TempDir()
			d := &Deployer{options: fakeOptions{runID: "run-123", runDir: runDir}}
			err := d.loadEnvironment()
			if wantErrContains != "" {
				Expect(err).To(MatchError(ContainSubstring(wantErrContains)))
				return
			}
			Expect(err).NotTo(HaveOccurred())
			Expect(d.parentContainerID).To(Equal("parent-1"))
			Expect(d.projectMemberEmail).To(Equal("owner@example.com"))
			Expect(d.projectID).To(BeEmpty())
			Expect(d.kubeconfigPath).To(Equal(filepath.Join(runDir, "kubeconfig")))
			Expect(d.serviceAccountKeyPath).To(Equal(filepath.Join(runDir, "service-account-key.json")))
		},
		Entry("missing service account", map[string]string{
			"STACKIT_PARENT_CONTAINER_ID": "parent-1",
		}, "STACKIT_SERVICE_ACCOUNT"),
		Entry("missing parent container", map[string]string{
			"STACKIT_SERVICE_ACCOUNT": validServiceAccountKey,
		}, "STACKIT_PARENT_CONTAINER_ID"),
		Entry("invalid service account key", map[string]string{
			"STACKIT_SERVICE_ACCOUNT":     "{}",
			"STACKIT_PARENT_CONTAINER_ID": "parent-1",
		}, "invalid STACKIT_SERVICE_ACCOUNT"),
		Entry("project id no longer required", map[string]string{
			"STACKIT_SERVICE_ACCOUNT":     validServiceAccountKey,
			"STACKIT_PARENT_CONTAINER_ID": "parent-1",
		}, ""),
	)

	It("reads optional endpoints", func() {
		for _, key := range envVarKeys {
			setEnvVar(key, "")
		}
		setEnvVar("STACKIT_SERVICE_ACCOUNT", validServiceAccountKey)
		setEnvVar("STACKIT_PARENT_CONTAINER_ID", "parent-1")
		setEnvVar("STACKIT_RESOURCE_MANAGER_ENDPOINT", "https://resource-manager.example.com")
		setEnvVar("STACKIT_SERVICE_ACCOUNT_ENDPOINT", "https://service-account.example.com")
		setEnvVar("STACKIT_AUTHORIZATION_ENDPOINT", "https://authorization.example.com")
		setEnvVar("STACKIT_SKE_ENDPOINT", "https://ske.example.com")

		runDir := GinkgoT().TempDir()
		d := &Deployer{options: fakeOptions{runID: "run-123", runDir: runDir}}
		Expect(d.loadEnvironment()).To(Succeed())

		Expect(d.projectMemberEmail).To(Equal("owner@example.com"))
		Expect(d.resourceManagerEndpoint).To(Equal("https://resource-manager.example.com"))
		Expect(d.serviceAccountEndpoint).To(Equal("https://service-account.example.com"))
		Expect(d.authorizationEndpoint).To(Equal("https://authorization.example.com"))
		Expect(d.skeEndpoint).To(Equal("https://ske.example.com"))
	})
})
