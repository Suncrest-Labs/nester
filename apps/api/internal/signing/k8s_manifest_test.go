package signing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionK8sManifest_EnforcesPodSecurityAndSignerNetworkPolicy(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "..", "..", "deploy", "production", "k8s-production.yaml")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", manifestPath, err)
	}
	content := string(raw)

	requiredSnippets := []string{
		"pod-security.kubernetes.io/enforce: restricted",
		"runAsNonRoot: true",
		"readOnlyRootFilesystem: true",
		"allowPrivilegeEscalation: false",
		"drop:\n                - ALL",
		"seccompProfile:\n              type: RuntimeDefault",
		"resources:\n            requests:",
		"limits:\n              cpu:",
		"@sha256:",
		"kind: NetworkPolicy",
		"name: default-deny-all",
		"name: nester-signer-isolation",
		"app: nester-signer",
		"app: nester-api",
		"port: 8443",
		"SIGNER_REQUIRE_KMS",
	}

	for _, snippet := range requiredSnippets {
		normalizedContent := strings.ReplaceAll(content, "\r\n", "\n")
		if !strings.Contains(normalizedContent, snippet) {
			t.Errorf("production k8s manifest missing required security control %q", snippet)
		}
	}

	if strings.Contains(content, ":latest") {
		t.Errorf("production k8s manifest must not use mutable :latest image tag")
	}
}
