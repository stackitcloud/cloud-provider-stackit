package kubetest2

import (
	"context"
	"errors"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	authorization "github.com/stackitcloud/stackit-sdk-go/services/authorization/v2api"
	serviceaccount "github.com/stackitcloud/stackit-sdk-go/services/serviceaccount/v2api"
	"k8s.io/apimachinery/pkg/util/wait"
)

var _ = Describe("matchesManagedServiceAccountEmail", func() {
	It("matches the managed service account prefix", func() {
		d := newTestDeployer()
		Expect(d.matchesManagedServiceAccountEmail(d.serviceAccountName() + "@sa.stackit.cloud")).To(BeTrue())
	})

	It("matches emails with generated suffixes", func() {
		d := newTestDeployer()
		Expect(d.matchesManagedServiceAccountEmail(d.serviceAccountName() + "-aBc2defg@sa.stackit.cloud")).To(BeTrue())
	})

	It("rejects longer prefixes without the generated suffix separator", func() {
		d := newTestDeployer()
		Expect(d.matchesManagedServiceAccountEmail(d.serviceAccountName() + "extra@sa.stackit.cloud")).To(BeFalse())
	})

	DescribeTable("rejects non-matching emails",
		func(email string) {
			d := newTestDeployer()
			Expect(d.matchesManagedServiceAccountEmail(email)).To(BeFalse())
		},
		Entry("empty email", ""),
		Entry("missing at sign", "kt2-no-separator"),
		Entry("different local prefix", "other-account@sa.stackit.cloud"),
	)
})

var _ = Describe("ensureServiceAccount", func() {
	It("reuses cached key and skips membership write", func() {
		d := newTestDeployer()
		d.projectID = "project-123"
		cachedKey := `{"credentials":{"privateKey":"cached"}}`
		Expect(os.WriteFile(d.serviceAccountKeyPath, []byte(cachedKey), 0o600)).To(Succeed())

		serviceAccountEmail := d.serviceAccountName() + "-aBc2defg@sa.stackit.cloud"
		serviceAccountClient := &fakeServiceAccountClient{
			listResult: []serviceaccount.ServiceAccount{
				*serviceAccountFixture(serviceAccountEmail, "project-123"),
			},
		}
		authorizationClient := &fakeAuthorizationClient{
			listMembersResult: []authorization.Member{
				*authorization.NewMember(childProjectSKERole, serviceAccountEmail),
				*authorization.NewMember(childProjectStorageRole, serviceAccountEmail),
			},
		}
		fakeSKE := &fakeSKEClient{}
		var receivedKey string

		d.serviceAccountClient = serviceAccountClient
		d.authorizationClient = authorizationClient
		d.skeClientFactory = func(_, serviceAccount, _ string) (skeClient, error) {
			receivedKey = serviceAccount
			return fakeSKE, nil
		}

		Expect(d.ensureServiceAccount(context.Background())).To(Succeed())
		Expect(receivedKey).To(Equal(cachedKey))
		Expect(authorizationClient.addCalls).To(Equal(0))
		Expect(serviceAccountClient.createKeyCalls).To(Equal(0))
	})

	It("creates key and adds membership", func() {
		d := newTestDeployer()
		d.projectID = "project-123"

		serviceAccountEmail := d.serviceAccountName() + "-aBc2defg@sa.stackit.cloud"
		serviceAccountClient := &fakeServiceAccountClient{
			listResult: []serviceaccount.ServiceAccount{
				*serviceAccountFixture(serviceAccountEmail, "project-123"),
			},
			createKeyResult: createServiceAccountKeyResponseFixture(serviceAccountEmail),
		}
		authorizationClient := &fakeAuthorizationClient{}
		var receivedKey string

		d.serviceAccountClient = serviceAccountClient
		d.authorizationClient = authorizationClient
		d.skeClientFactory = func(_, serviceAccount, _ string) (skeClient, error) {
			receivedKey = serviceAccount
			return &fakeSKEClient{}, nil
		}

		Expect(d.ensureServiceAccount(context.Background())).To(Succeed())
		Expect(authorizationClient.addCalls).To(Equal(1))
		Expect(serviceAccountClient.createKeyCalls).To(Equal(1))
		Expect(authorizationClient.lastAddedType).To(Equal(projectResourceType))
		Expect(authorizationClient.lastAddedID).To(Equal("project-123"))
		Expect(authorizationClient.lastAddedMembers).To(ConsistOf(
			*authorization.NewMember(childProjectSKERole, serviceAccountEmail),
			*authorization.NewMember(childProjectStorageRole, serviceAccountEmail),
		))

		keyBytes, err := os.ReadFile(d.serviceAccountKeyPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(receivedKey).To(Equal(string(keyBytes)))
		Expect(string(keyBytes)).To(ContainSubstring(`"privateKey":"PRIVATE"`))

		info, err := os.Stat(d.serviceAccountKeyPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
	})

	It("creates the managed service account when none exists", func() {
		d := newTestDeployer()
		d.projectID = "project-123"

		serviceAccountEmail := d.serviceAccountName() + "-aBc2defg@sa.stackit.cloud"
		serviceAccountClient := &fakeServiceAccountClient{
			createResult:    serviceAccountFixture(serviceAccountEmail, "project-123"),
			createKeyResult: createServiceAccountKeyResponseFixture(serviceAccountEmail),
		}
		authorizationClient := &fakeAuthorizationClient{}
		var receivedKey string

		d.serviceAccountClient = serviceAccountClient
		d.authorizationClient = authorizationClient
		d.skeClientFactory = func(_, serviceAccount, _ string) (skeClient, error) {
			receivedKey = serviceAccount
			return &fakeSKEClient{}, nil
		}

		Expect(d.ensureServiceAccount(context.Background())).To(Succeed())
		Expect(serviceAccountClient.createCalls).To(Equal(1))
		Expect(serviceAccountClient.listCalls).To(Equal(1))
		Expect(serviceAccountClient.createKeyCalls).To(Equal(1))
		Expect(authorizationClient.addCalls).To(Equal(1))
		Expect(authorizationClient.lastAddedMembers).To(ConsistOf(
			*authorization.NewMember(childProjectSKERole, serviceAccountEmail),
			*authorization.NewMember(childProjectStorageRole, serviceAccountEmail),
		))
		Expect(serviceAccountClient.lastCreatedName).To(Equal(d.serviceAccountName()))
		Expect(receivedKey).To(ContainSubstring(`"privateKey":"PRIVATE"`))
	})
})

var _ = Describe("retryWithBackoff", func() {
	It("retries until success", func() {
		calls := 0
		result, err := retryWithBackoff(context.Background(), wait.Backoff{Duration: 0, Factor: 1, Steps: 3}, func() (string, error) {
			calls++
			if calls < 2 {
				return "", errors.New("transient")
			}
			return "done", nil
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("done"))
		Expect(calls).To(Equal(2))
	})

	It("returns last error when exhausted", func() {
		calls := 0
		_, err := retryWithBackoff(context.Background(), wait.Backoff{Duration: 0, Factor: 1, Steps: 3}, func() (int, error) {
			calls++
			return 0, errors.New("always fails")
		})
		Expect(err).To(HaveOccurred())
		Expect(calls).To(Equal(3))
	})
})
