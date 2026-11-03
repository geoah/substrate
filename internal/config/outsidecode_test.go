package config

import (
	"strings"
	"testing"
)

func TestOutsideCodeRejectsUnknownPolicy(t *testing.T) {
	for _, value := range []string{"deny", "ALLOW", "false"} {
		if err := (Config{OutsideCode: value}).Validate(); err == nil || !strings.Contains(err.Error(), "SUBSTRATE_OUTSIDE_CODE") {
			t.Fatalf("%s: %v", value, err)
		}
	}
}
