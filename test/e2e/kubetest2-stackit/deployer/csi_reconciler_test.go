package deployer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCSICloudConfigUsesNestedShape(t *testing.T) {
	content, err := buildCSICloudConfig(&csiInstallConfig{
		ProjectID:      "project-id",
		Region:         "eu01",
		RescanOnResize: true,
	})
	if err != nil {
		t.Fatalf("buildCSICloudConfig() returned error: %v", err)
	}
	if !strings.Contains(content, "global:\n") || !strings.Contains(content, "projectId: project-id") {
		t.Fatalf("expected nested global projectId in cloud config, got:\n%s", content)
	}
	if !strings.Contains(content, "blockStorage:\n") || !strings.Contains(content, "rescanOnResize: true") {
		t.Fatalf("expected blockStorage.rescanOnResize in cloud config, got:\n%s", content)
	}
}

func TestWriteCSITestDriverConfigUsesLowercaseCapabilityKeys(t *testing.T) {
	path := t.TempDir() + "/csi-testdriver.yaml"

	err := writeCSITestDriverConfig(path)
	if err != nil {
		t.Fatalf("writeCSITestDriverConfig() returned error: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed reading generated CSI testdriver config: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "block: true") {
		t.Fatalf("expected lowercase block capability key in generated testdriver config, got:\n%s", text)
	}
	if strings.Contains(text, "Block: true") {
		t.Fatalf("unexpected uppercase block capability key in generated testdriver config, got:\n%s", text)
	}
}

func TestWriteKustomizeOverlayAppliesPlaceholders(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &csiInstallConfig{
		ImageName: "test-image",
		ImageTag:  "v1.0.0",
	}

	if err := writeKustomizeOverlay(tmpDir, cfg); err != nil {
		t.Fatalf("writeKustomizeOverlay() returned error: %v", err)
	}

	kustomizationPath := filepath.Join(tmpDir, "kustomization.yaml")
	content, err := os.ReadFile(kustomizationPath)
	if err != nil {
		t.Fatalf("failed reading kustomization.yaml: %v", err)
	}

	kustomizationContent := string(content)
	if strings.Contains(kustomizationContent, "IMAGE_NAME_PLACEHOLDER") {
		t.Fatalf("kustomization.yaml still contains IMAGE_NAME_PLACEHOLDER")
	}
	if strings.Contains(kustomizationContent, "IMAGE_TAG_PLACEHOLDER") {
		t.Fatalf("kustomization.yaml still contains IMAGE_TAG_PLACEHOLDER")
	}
	if strings.Contains(kustomizationContent, "CSI_DRIVER_NAME_PLACEHOLDER") {
		t.Fatalf("kustomization.yaml still contains CSI_DRIVER_NAME_PLACEHOLDER")
	}
	if strings.Contains(kustomizationContent, "CSI_CLASS_NAME_PLACEHOLDER") {
		t.Fatalf("kustomization.yaml still contains CSI_CLASS_NAME_PLACEHOLDER")
	}
	if !strings.Contains(kustomizationContent, "value: "+kustomizeTestDriverName) {
		t.Fatalf("expected driver name %q in kustomization, got:\n%s", kustomizeTestDriverName, kustomizationContent)
	}
	if !strings.Contains(kustomizationContent, "value: "+kustomizeTestClassName) {
		t.Fatalf("expected class name %q in kustomization, got:\n%s", kustomizeTestClassName, kustomizationContent)
	}
	if !strings.Contains(kustomizationContent, "newName: test-image") {
		t.Fatalf("expected newName: test-image in kustomization, got:\n%s", kustomizationContent)
	}
	if !strings.Contains(kustomizationContent, "newTag: v1.0.0") {
		t.Fatalf("expected newTag: v1.0.0 in kustomization, got:\n%s", kustomizationContent)
	}
	if !strings.Contains(kustomizationContent, "github.com/kubernetes-csi/external-snapshotter") {
		t.Fatalf("expected remote snapshot-controller reference, got:\n%s", kustomizationContent)
	}
	if !strings.Contains(kustomizationContent, "deploy/csi-plugin") {
		t.Fatalf("expected csi-plugin reference, got:\n%s", kustomizationContent)
	}
}

func TestIndentYAML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		spaces   int
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			spaces:   2,
			expected: "",
		},
		{
			name:     "single line",
			input:    "foo",
			spaces:   2,
			expected: "  foo",
		},
		{
			name:     "multiple lines",
			input:    "foo\nbar\nbaz",
			spaces:   4,
			expected: "    foo\n    bar\n    baz",
		},
		{
			name:     "empty lines preserved",
			input:    "foo\n\nbar",
			spaces:   2,
			expected: "  foo\n\n  bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := indentYAML(tt.input, tt.spaces)
			if result != tt.expected {
				t.Fatalf("indentYAML(%q, %d) = %q, want %q", tt.input, tt.spaces, result, tt.expected)
			}
		})
	}
}
