package engine

import (
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/sandbox"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestOutsideRefuseOverridesInsecureException(t *testing.T) {
	s := &service{outsideCode: "refuse", unconfinedOutsideCode: true}
	if err := s.admitOutsideCode(); err == nil || !strings.Contains(err.Error(), "SUBSTRATE_OUTSIDE_CODE=refuse") {
		t.Fatalf("got %v", err)
	}
}

func TestOutsideCodeDiffIncludesPermissionsAndSource(t *testing.T) {
	before := map[string]any{"source": "return 1\n", "permissions": map[string]any{"network": []string{"api.example.com"}}}
	after := map[string]any{"source": "return 2\n", "permissions": map[string]any{"network": []string{"other.example.com"}}}
	diff, err := codeDiff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"return 1", "return 2", "api.example.com", "other.example.com"} {
		if !strings.Contains(diff, want) {
			t.Fatalf("missing %s: %s", want, diff)
		}
	}
	if diff, err := codeDiff(before, before); err != nil || diff != "" {
		t.Fatalf("unchanged code: %s %v", diff, err)
	}
	for _, kind := range []string{vocabulary.DocFunction, vocabulary.DocAgent, vocabulary.DocRecordMapping} {
		if codeDeclaration(vocabulary.Document{Kind: kind, Data: before}) == nil {
			t.Errorf("missing %s", kind)
		}
	}
}

func TestOutsideOAuthRequiresHTTPSAndMayUseAnotherAuthority(t *testing.T) {
	for _, endpoint := range []string{"authorizationEndpoint", "tokenEndpoint", "revocationEndpoint", "emailEndpoint"} {
		for _, scheme := range []string{"https", "http"} {
			docs := []vocabulary.Document{{Kind: vocabulary.DocBundle, ID: "publisher.example.com/addon", Data: map[string]any{"oauth2": map[string]any{endpoint: scheme + "://provider.example.org/endpoint"}}}}
			code, err := validateOutsideCode(docs, nil)
			if scheme == "https" && (err != nil || !code) {
				t.Fatalf("HTTPS off-authority refused: %v", err)
			}
			if scheme == "http" && err == nil {
				t.Fatalf("HTTP %s admitted", endpoint)
			}
		}
	}
}

func TestOutsideKindsOnlyHasNoCode(t *testing.T) {
	code, err := validateOutsideCode([]vocabulary.Document{{Kind: vocabulary.DocKind, Data: map[string]any{}}}, nil)
	if err != nil || code {
		t.Fatalf("kinds only: %v %v", code, err)
	}
}

func TestOutsideConfinementRefusalAndExplicitException(t *testing.T) {
	s := &service{}
	report := runner.Shared.Sandbox().Report()
	if report.Supported() && report.FS() && report.Seccomp && report.ConnectGate && runner.Shared.Sandbox().Mode() != sandbox.ModeOff {
		t.Skip("this host provides confinement")
	}
	if err := s.admitOutsideCode(); err == nil {
		t.Fatal("unconfined outside code admitted")
	}
	s.unconfinedOutsideCode = true
	if err := s.admitOutsideCode(); err != nil {
		t.Fatal(err)
	}
}
