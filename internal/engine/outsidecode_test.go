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
	for name, admit := range map[string]func() error{"bodies": s.admitOutsideBodies, "policy": s.admitOutsidePolicy} {
		if err := admit(); err == nil || !strings.Contains(err.Error(), "SUBSTRATE_OUTSIDE_CODE=refuse") {
			t.Fatalf("%s: got %v", name, err)
		}
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
			code, bodies, err := validateOutsideCode(docs, nil)
			if scheme == "https" && (err != nil || !code || bodies) {
				t.Fatalf("HTTPS off-authority refused: %v", err)
			}
			if scheme == "http" && err == nil {
				t.Fatalf("HTTP %s admitted", endpoint)
			}
		}
	}
}

func TestOutsideKindsOnlyHasNoCode(t *testing.T) {
	code, bodies, err := validateOutsideCode([]vocabulary.Document{{Kind: vocabulary.DocKind, Data: map[string]any{}}}, nil)
	if err != nil || code || bodies {
		t.Fatalf("kinds only: %v %v %v", code, bodies, err)
	}
}

// An outside agent, trigger or mapping is outside code the operator's
// policy decides, and none is a body the sandbox runs: only a function needs
// a host that confines it, so a kernel without Landlock still takes an
// outside package of agents.
func TestOnlyAnOutsideFunctionNeedsTheSandbox(t *testing.T) {
	for kind, wantBodies := range map[string]bool{vocabulary.DocFunction: true, vocabulary.DocAgent: false, vocabulary.DocRecordMapping: false} {
		code, bodies, err := validateOutsideCode([]vocabulary.Document{{Kind: kind, ID: "publisher.example.com/pkg/x", Data: map[string]any{}}}, nil)
		if err != nil || !code || bodies != wantBodies {
			t.Errorf("%s: code %v bodies %v err %v, want code and bodies %v", kind, code, bodies, err, wantBodies)
		}
	}
	s := &service{}
	if err := s.admitOutsidePolicy(); err != nil {
		t.Fatalf("the default policy refused outside code: %v", err)
	}
}

func TestOutsideConfinementRefusalAndExplicitException(t *testing.T) {
	s := &service{}
	report := runner.Shared.Sandbox().Report()
	if report.Supported() && report.FS() && report.Seccomp && report.ConnectGate && runner.Shared.Sandbox().Mode() != sandbox.ModeOff {
		t.Skip("this host provides confinement")
	}
	if err := s.admitOutsideBodies(); err == nil {
		t.Fatal("unconfined outside code admitted")
	}
	s.unconfinedOutsideCode = true
	if err := s.admitOutsideBodies(); err != nil {
		t.Fatal(err)
	}
}
