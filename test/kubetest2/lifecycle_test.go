package kubetest2

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	oapierror "github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	authorization "github.com/stackitcloud/stackit-sdk-go/services/authorization/v2api"
	resourcemanager "github.com/stackitcloud/stackit-sdk-go/services/resourcemanager/v0api"
	serviceaccount "github.com/stackitcloud/stackit-sdk-go/services/serviceaccount/v2api"
	serviceenablement "github.com/stackitcloud/stackit-sdk-go/services/serviceenablement/v2api"
	ske "github.com/stackitcloud/stackit-sdk-go/services/ske/v2api"
)

var _ = Describe("Up", func() {
	It("uses discovered project and writes kubeconfig", func() {
		d := newTestDeployer()
		configureValidUpInputs(d)

		cachedKey := `{"credentials":{"privateKey":"cached"}}`
		Expect(os.WriteFile(d.serviceAccountKeyPath, []byte(cachedKey), 0o600)).To(Succeed())

		d.projectClient = &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{
				*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
			},
		}
		serviceAccountEmail := d.serviceAccountName() + "@sa.stackit.cloud"
		d.serviceAccountClient = &fakeServiceAccountClient{
			listResult: []serviceaccount.ServiceAccount{
				*serviceAccountFixture(serviceAccountEmail),
			},
		}
		d.authorizationClient = &fakeAuthorizationClient{
			listMembersResult: []authorization.Member{
				*authorization.NewMember(childProjectSKERole, serviceAccountEmail),
				*authorization.NewMember(childProjectStorageRole, serviceAccountEmail),
			},
		}
		d.serviceEnablementClient = &fakeServiceEnablementClient{getStatusResult: serviceenablement.NewServiceStatus()}

		fakeSKE := &fakeSKEClient{
			providerOptions:      providerOptionsFixture(),
			createOrUpdateResult: ske.NewClusterWithDefaults(),
			waitReadyResult:      ske.NewClusterWithDefaults(),
			kubeconfigResult: func() *ske.Kubeconfig {
				cfg := ske.NewKubeconfig()
				cfg.SetKubeconfig("apiVersion: v1\n")
				return cfg
			}(),
		}
		d.skeClientFactory = func(_, serviceAccount, _ string) (skeClient, error) {
			Expect(serviceAccount).To(Equal(cachedKey))
			return fakeSKE, nil
		}

		Expect(d.Up()).To(Succeed())
		Expect(fakeSKE.lastCreateProjectID).To(Equal("project-123"))
		Expect(fakeSKE.lastCreateClusterName).To(Equal(d.clusterName()))

		kubeconfigBytes, err := os.ReadFile(d.kubeconfigPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(kubeconfigBytes)).To(Equal("apiVersion: v1\n"))
	})

	It("acquires test package with resolved kubernetes version when ShouldTest is true", func() {
		d := newTestDeployer()
		configureValidUpInputs(d)
		d.kubernetesVersion = "1.36"
		d.nodeImageName = "flatcar"
		d.nodeImageVersion = ""
		d.options = fakeOptions{
			runID:      "run-123",
			runDir:     d.options.RunDir(),
			shouldUp:   true,
			shouldTest: true,
		}

		cachedKey := `{"credentials":{"privateKey":"cached"}}`
		Expect(os.WriteFile(d.serviceAccountKeyPath, []byte(cachedKey), 0o600)).To(Succeed())

		d.projectClient = &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{
				*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
			},
		}
		serviceAccountEmail := d.serviceAccountName() + "@sa.stackit.cloud"
		d.serviceAccountClient = &fakeServiceAccountClient{
			listResult: []serviceaccount.ServiceAccount{
				*serviceAccountFixture(serviceAccountEmail),
			},
		}
		d.authorizationClient = &fakeAuthorizationClient{
			listMembersResult: []authorization.Member{
				*authorization.NewMember(childProjectSKERole, serviceAccountEmail),
				*authorization.NewMember(childProjectStorageRole, serviceAccountEmail),
			},
		}
		d.serviceEnablementClient = &fakeServiceEnablementClient{getStatusResult: serviceenablement.NewServiceStatus()}

		fakeSKE := &fakeSKEClient{
			providerOptions:      multiVersionProviderOptionsFixture(),
			createOrUpdateResult: ske.NewClusterWithDefaults(),
			waitReadyResult:      ske.NewClusterWithDefaults(),
			kubeconfigResult: func() *ske.Kubeconfig {
				cfg := ske.NewKubeconfig()
				cfg.SetKubeconfig("apiVersion: v1\n")
				return cfg
			}(),
		}
		d.skeClientFactory = func(_, _, _ string) (skeClient, error) {
			return fakeSKE, nil
		}

		var acquiredVersion, acquiredRunDir string
		d.testPackageAcquirer = func(_ context.Context, version, runDir string) error {
			acquiredVersion = version
			acquiredRunDir = runDir
			return nil
		}

		Expect(d.Up()).To(Succeed())
		Expect(acquiredVersion).To(Equal("1.36.5"))
		Expect(acquiredRunDir).To(Equal(d.options.RunDir()))
	})
})

var _ = Describe("acquireTestPackageFromURL", func() {
	It("downloads and extracts e2e.test, ginkgo, and kubectl into runDir", func() {
		var tarBuf bytes.Buffer
		gzw := gzip.NewWriter(&tarBuf)
		tw := tar.NewWriter(gzw)
		for name, body := range map[string]string{
			"kubernetes/test/bin/e2e.test": "e2e-binary",
			"kubernetes/test/bin/ginkgo":   "ginkgo-binary",
		} {
			Expect(tw.WriteHeader(&tar.Header{
				Name: name,
				Mode: 0o755,
				Size: int64(len(body)),
			})).To(Succeed())
			_, err := tw.Write([]byte(body))
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(tw.Close()).To(Succeed())
		Expect(gzw.Close()).To(Succeed())

		tarBytes := tarBuf.Bytes()
		tarSum := sha256.Sum256(tarBytes)
		tarSHA := hex.EncodeToString(tarSum[:])

		kubectlBytes := []byte("kubectl-binary")
		kubectlSum := sha256.Sum256(kubectlBytes)
		kubectlSHA := hex.EncodeToString(kubectlSum[:])

		tarPath := fmt.Sprintf("/release/v1.36.5/kubernetes-test-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
		kubectlPath := fmt.Sprintf("/release/v1.36.5/bin/%s/%s/kubectl", runtime.GOOS, runtime.GOARCH)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case tarPath:
				_, _ = w.Write(tarBytes)
			case tarPath + ".sha256":
				_, _ = w.Write([]byte(tarSHA))
			case kubectlPath:
				_, _ = w.Write(kubectlBytes)
			case kubectlPath + ".sha256":
				_, _ = w.Write([]byte(kubectlSHA))
			default:
				http.NotFound(w, r)
			}
		}))
		DeferCleanup(srv.Close)

		runDir := GinkgoT().TempDir()
		cacheDir := GinkgoT().TempDir()
		Expect(acquireTestPackageFromURL(context.Background(), srv.URL, "1.36.5", runDir, cacheDir)).To(Succeed())

		for name, want := range map[string]string{
			"e2e.test": "e2e-binary",
			"ginkgo":   "ginkgo-binary",
			"kubectl":  "kubectl-binary",
		} {
			got, err := os.ReadFile(filepath.Join(runDir, name))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(got)).To(Equal(want))
		}
	})

	It("fails when sha256 verification fails", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if filepath.Ext(r.URL.Path) == ".sha256" {
				_, _ = w.Write([]byte("deadbeef"))
				return
			}
			_, _ = w.Write([]byte("corrupt-payload"))
		}))
		DeferCleanup(srv.Close)

		err := acquireTestPackageFromURL(context.Background(), srv.URL, "1.36.5", GinkgoT().TempDir(), GinkgoT().TempDir())
		Expect(err).To(MatchError(ContainSubstring("sha256 mismatch")))
	})
})

var _ = Describe("Down", func() {
	It("succeeds when project is missing", func() {
		d := newTestDeployer()
		projectClient := &fakeProjectClient{}
		d.projectClient = projectClient

		Expect(d.Down()).To(Succeed())
		Expect(projectClient.deleteCalls).To(Equal(0))
	})

	It("deletes project", func() {
		d := newTestDeployer()
		d.projectClient = &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{
				*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
			},
		}

		Expect(d.Down()).To(Succeed())
		projectClient := d.projectClient.(*fakeProjectClient)
		Expect(projectClient.deleteCalls).To(Equal(1))
		Expect(projectClient.waitDeletedCalls).To(Equal(1))
	})

	It("skips delete when project is already deleting", func() {
		d := newTestDeployer()
		deletingProject := projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels())
		deletingProject.SetLifecycleState(resourcemanager.LIFECYCLESTATE_DELETING)

		projectClient := &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{*deletingProject},
		}
		d.projectClient = projectClient

		Expect(d.Down()).To(Succeed())
		Expect(projectClient.deleteCalls).To(Equal(0))
		Expect(projectClient.waitDeletedCalls).To(Equal(1))
	})
})

var _ = Describe("IsUp", func() {
	It("returns false when no project", func() {
		d := newTestDeployer()
		d.projectClient = &fakeProjectClient{}

		isUp, err := d.IsUp()
		Expect(err).NotTo(HaveOccurred())
		Expect(isUp).To(BeFalse())
	})

	It("returns false when project is deleting", func() {
		d := newTestDeployer()
		deletingProject := projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels())
		deletingProject.SetLifecycleState(resourcemanager.LIFECYCLESTATE_DELETING)

		d.projectClient = &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{*deletingProject},
		}

		isUp, err := d.IsUp()
		Expect(err).NotTo(HaveOccurred())
		Expect(isUp).To(BeFalse())
	})

	It("returns error when project without cached key", func() {
		d := newTestDeployer()
		d.projectClient = &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{
				*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
			},
		}

		_, err := d.IsUp()
		Expect(err).To(MatchError(ContainSubstring("child service-account key cache")))
	})

	It("queries cluster when project with cached key", func() {
		d := newTestDeployer()
		d.projectClient = &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{
				*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
			},
		}
		cachedKey := `{"credentials":{"privateKey":"cached"}}`
		Expect(os.WriteFile(d.serviceAccountKeyPath, []byte(cachedKey), 0o600)).To(Succeed())

		fakeSKE := &fakeSKEClient{
			getClusterResult: healthyClusterFixture(),
		}
		d.skeClientFactory = func(_, serviceAccount, _ string) (skeClient, error) {
			Expect(serviceAccount).To(Equal(cachedKey))
			return fakeSKE, nil
		}

		isUp, err := d.IsUp()
		Expect(err).NotTo(HaveOccurred())
		Expect(isUp).To(BeTrue())
		Expect(fakeSKE.lastGetProjectID).To(Equal("project-123"))
	})

	It("returns false when cluster missing", func() {
		d := newTestDeployer()
		d.projectClient = &fakeProjectClient{
			listProjectsResult: []resourcemanager.Project{
				*projectFixture(d.projectName(), "project-123", "container-123", d.managedProjectLabels()),
			},
		}
		cachedKey := `{"credentials":{"privateKey":"cached"}}`
		Expect(os.WriteFile(d.serviceAccountKeyPath, []byte(cachedKey), 0o600)).To(Succeed())

		fakeSKE := &fakeSKEClient{
			getClusterErr: &oapierror.GenericOpenAPIError{StatusCode: http.StatusNotFound},
		}
		d.skeClientFactory = func(_, _, _ string) (skeClient, error) {
			return fakeSKE, nil
		}

		isUp, err := d.IsUp()
		Expect(err).NotTo(HaveOccurred())
		Expect(isUp).To(BeFalse())
	})
})
