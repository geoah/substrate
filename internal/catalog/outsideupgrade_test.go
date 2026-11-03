package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOutsideTriggerDiffOmitsOwnerSettings(t *testing.T) {
	code, err := triggerCode(map[string]any{"callable": "substrate.reamde.dev/core/function/publisher.example.com/outside/probe", "enabled": false, "source": map[string]any{"webhook": map[string]any{"key": "fixture-private-value", "maxBody": 2048}}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(code)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "fixture-private-value") || strings.Contains(string(raw), "enabled") {
		t.Fatal("owner settings escaped into code review")
	}
	if !strings.Contains(string(raw), "maxBody") {
		t.Fatal("code was hidden with settings")
	}
}
