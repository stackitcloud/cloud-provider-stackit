package kubetest2

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var _ = Describe("ensureCSI", func() {
	It("stages manifests with rendered cloud config and calls csiApplier", func() {
		d := newTestDeployer()
		configureValidUpInputs(d)
		d.projectID = "project-123"
		d.iaasEndpoint = "https://iaas.api.qa.stackit.cloud"

		serviceAccountKey := `{"credentials":{"privateKey":"dummy-private-key"}}`
		Expect(os.WriteFile(d.serviceAccountKeyPath, []byte(serviceAccountKey), 0o600)).To(Succeed())

		var recordedKubeconfig string
		var recordedStagedDir string
		d.csiApplier = func(_ context.Context, kubeconfigPath, stagedKustomizeDir string) error {
			recordedKubeconfig = kubeconfigPath
			recordedStagedDir = stagedKustomizeDir
			return nil
		}

		Expect(d.ensureCSI(context.Background())).To(Succeed())

		Expect(recordedKubeconfig).To(Equal(d.kubeconfigPath))
		Expect(recordedStagedDir).To(Equal(filepath.Join(d.options.RunDir(), "manifests", "test", "kustomize")))

		// Verify substitutions in staged files
		stagedKustomizeDir := filepath.Join(d.options.RunDir(), "manifests", "test", "kustomize")

		kustomizationBytes, err := os.ReadFile(filepath.Join(stagedKustomizeDir, "kustomization.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(kustomizationBytes)).To(ContainSubstring("newName: " + d.csiImageName))
		Expect(string(kustomizationBytes)).To(ContainSubstring("newTag: " + d.csiImageTag))
		Expect(string(kustomizationBytes)).NotTo(ContainSubstring("REPLACE_WITH_IMAGE_NAME"))
		Expect(string(kustomizationBytes)).NotTo(ContainSubstring("REPLACE_WITH_TAG"))

		cloudConfigBytes, err := os.ReadFile(filepath.Join(stagedKustomizeDir, "cloud-config.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(cloudConfigBytes)).To(ContainSubstring("projectId: project-123"))
		Expect(string(cloudConfigBytes)).To(ContainSubstring("region: " + d.region))
		Expect(string(cloudConfigBytes)).To(ContainSubstring("apiEndpoints:"))
		Expect(string(cloudConfigBytes)).To(ContainSubstring("iaasApi: " + d.iaasEndpoint))
		Expect(string(cloudConfigBytes)).To(ContainSubstring("name: " + csiConfigMapName))
		Expect(string(cloudConfigBytes)).To(ContainSubstring("namespace: " + csiConfigNamespace))
		Expect(string(cloudConfigBytes)).NotTo(ContainSubstring("REPLACE_WITH_PROJECTID"))
		Expect(string(cloudConfigBytes)).NotTo(ContainSubstring("REPLACE_WITH_OPTIONAL_IAAS_API"))

		cloudSecretBytes, err := os.ReadFile(filepath.Join(stagedKustomizeDir, "cloud-secret.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(cloudSecretBytes)).To(ContainSubstring(serviceAccountKey))
		Expect(string(cloudSecretBytes)).NotTo(ContainSubstring("REPLACE_WITH_SERVICEACCOUNT_JSON"))

		testDriverBytes, err := os.ReadFile(filepath.Join(d.options.RunDir(), csiTestDriverFileName))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(testDriverBytes)).To(ContainSubstring("FromExistingClassName: " + kubetest2CSIStorageClassName))
		Expect(string(testDriverBytes)).To(ContainSubstring("Name: " + kubetest2CSIDriverName))
		Expect(string(testDriverBytes)).NotTo(ContainSubstring("FromExistingClassName: premium-perf4-stackit\n"))
		Expect(string(testDriverBytes)).NotTo(ContainSubstring("Name: block-storage.csi.stackit.cloud"))

		// Verify that in-process renderKustomize parses the staged overlay correctly into unstructured objects
		objects, err := renderKustomize(stagedKustomizeDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(objects).NotTo(BeEmpty())

		var foundControllerPlugin, foundNodePlugin bool
		for _, obj := range objects {
			if obj.GetKind() == "Deployment" && obj.GetName() == "csi-stackit-controllerplugin" {
				foundControllerPlugin = true
			}
			if obj.GetKind() == "DaemonSet" && obj.GetName() == "csi-stackit-nodeplugin" {
				foundNodePlugin = true
			}
		}
		Expect(foundControllerPlugin).To(BeTrue(), "expected csi-stackit-controllerplugin in rendered objects")
		Expect(foundNodePlugin).To(BeTrue(), "expected csi-stackit-nodeplugin in rendered objects")
	})

	It("does not write iaas endpoint override when not set", func() {
		d := newTestDeployer()
		configureValidUpInputs(d)
		d.projectID = "project-123"

		serviceAccountKey := `{"credentials":{"privateKey":"dummy-private-key"}}`
		Expect(os.WriteFile(d.serviceAccountKeyPath, []byte(serviceAccountKey), 0o600)).To(Succeed())

		d.csiApplier = func(_ context.Context, _, _ string) error {
			return nil
		}

		Expect(d.ensureCSI(context.Background())).To(Succeed())

		cloudConfigBytes, err := os.ReadFile(filepath.Join(d.options.RunDir(), "manifests", "test", "kustomize", "cloud-config.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(cloudConfigBytes)).NotTo(ContainSubstring("apiEndpoints:"))
		Expect(string(cloudConfigBytes)).NotTo(ContainSubstring("iaasApi:"))
		cloudSecretBytes, err := os.ReadFile(filepath.Join(d.options.RunDir(), "manifests", "test", "kustomize", "cloud-secret.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(cloudSecretBytes)).To(ContainSubstring(serviceAccountKey))
	})

	It("fails when cached service account key is missing", func() {
		d := newTestDeployer()
		configureValidUpInputs(d)
		d.projectID = "project-123"

		_ = os.Remove(d.serviceAccountKeyPath)

		err := d.ensureCSI(context.Background())
		Expect(err).To(MatchError(ContainSubstring("cached child service-account key is missing or empty")))
	})

	It("fails when csiApplier returns an error", func() {
		d := newTestDeployer()
		configureValidUpInputs(d)
		d.projectID = "project-123"

		serviceAccountKey := `{"credentials":{"privateKey":"dummy"}}`
		Expect(os.WriteFile(d.serviceAccountKeyPath, []byte(serviceAccountKey), 0o600)).To(Succeed())

		d.csiApplier = func(_ context.Context, _, _ string) error {
			return os.ErrInvalid
		}

		err := d.ensureCSI(context.Background())
		Expect(err).To(MatchError(ContainSubstring("apply CSI manifests")))
	})
})

var _ = Describe("renderCSICloudConfigManifest", func() {
	It("renders the optional iaas endpoint only when configured", func() {
		rendered, err := renderCSICloudConfigManifest("project-123", "eu01", "https://iaas.example.com")
		Expect(err).NotTo(HaveOccurred())

		text := string(rendered)
		Expect(text).To(ContainSubstring("name: " + csiConfigMapName))
		Expect(text).To(ContainSubstring("namespace: " + csiConfigNamespace))
		Expect(text).To(ContainSubstring("projectId: project-123"))
		Expect(text).To(ContainSubstring("region: eu01"))
		Expect(text).To(ContainSubstring("apiEndpoints:"))
		Expect(text).To(ContainSubstring("iaasApi: https://iaas.example.com"))
		Expect(text).To(ContainSubstring("rescanOnResize: true"))
	})

	It("omits apiEndpoints when no iaas endpoint override is configured", func() {
		rendered, err := renderCSICloudConfigManifest("project-123", "eu01", "")
		Expect(err).NotTo(HaveOccurred())

		text := string(rendered)
		Expect(text).To(ContainSubstring("projectId: project-123"))
		Expect(text).To(ContainSubstring("region: eu01"))
		Expect(text).NotTo(ContainSubstring("apiEndpoints:"))
		Expect(text).NotTo(ContainSubstring("iaasApi:"))
	})
})

var _ = Describe("renderCSITestDriverConfig", func() {
	It("rewrites the checked-in testdriver for the kubetest2 CSI overlay", func() {
		templatePath, err := resolveManifestPath(csiTestDriverTemplatePath)
		Expect(err).NotTo(HaveOccurred())

		rendered, err := renderCSITestDriverConfig(templatePath)
		Expect(err).NotTo(HaveOccurred())

		text := string(rendered)
		Expect(text).To(ContainSubstring("FromExistingClassName: " + kubetest2CSIStorageClassName))
		Expect(text).To(ContainSubstring("Name: " + kubetest2CSIDriverName))
		Expect(text).To(ContainSubstring("SnapshotClass:"))
		Expect(text).To(ContainSubstring("SupportedFsType:"))
		Expect(text).To(ContainSubstring("controllerExpansion: true"))
		Expect(strings.Count(text, kubetest2CSIStorageClassName)).To(Equal(1))
	})
})

var _ = Describe("waitForDeploymentReady", func() {
	It("succeeds when deployment is ready", func() {
		replicas := int32(1)
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "csi-stackit-controllerplugin",
				Namespace:  "kube-system",
				Generation: 1,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
			},
			Status: appsv1.DeploymentStatus{
				ObservedGeneration:  1,
				UpdatedReplicas:     1,
				AvailableReplicas:   1,
				UnavailableReplicas: 0,
			},
		}

		fakeClientset := fake.NewClientset(deploy)
		err := waitForDeploymentReady(context.Background(), fakeClientset, "kube-system", "csi-stackit-controllerplugin", timeoutDuration)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("waitForDaemonSetReady", func() {
	It("succeeds when daemonset is ready", func() {
		ds := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "csi-stackit-nodeplugin",
				Namespace:  "kube-system",
				Generation: 1,
			},
			Status: appsv1.DaemonSetStatus{
				ObservedGeneration:     1,
				DesiredNumberScheduled: 2,
				UpdatedNumberScheduled: 2,
				NumberAvailable:        2,
				NumberUnavailable:      0,
			},
		}

		fakeClientset := fake.NewClientset(ds)
		err := waitForDaemonSetReady(context.Background(), fakeClientset, "kube-system", "csi-stackit-nodeplugin", timeoutDuration)
		Expect(err).NotTo(HaveOccurred())
	})
})

const timeoutDuration = 2 * csiPollInterval
