package engine

import (
	"strings"
	"testing"
	"time"
)

// rfcSeed is the RFC 6238 Appendix B SHA-1 seed ("12345678901234567890").
var rfcSeed = totpEncoding.EncodeToString([]byte("12345678901234567890"))

// TestTOTPRFC6238Vectors is the published Appendix B table for SHA-1; the
// expected values are 8 digits, this implementation emits their low 6.
func TestTOTPRFC6238Vectors(t *testing.T) {
	for _, tc := range []struct {
		unix   int64
		eight  string
		wantSt int64
	}{
		{59, "94287082", 1},
		{1111111109, "07081804", 37037036},
		{1111111111, "14050471", 37037037},
		{1234567890, "89005924", 41152263},
		{2000000000, "69279037", 66666666},
		{20000000000, "65353130", 666666666},
	} {
		at := time.Unix(tc.unix, 0).UTC()
		if got := TOTPStep(at); got != tc.wantSt {
			t.Fatalf("TOTPStep(%d) = %d, want %d", tc.unix, got, tc.wantSt)
		}
		want := tc.eight[len(tc.eight)-totpDigits:]
		got, err := TOTPCode(rfcSeed, TOTPStep(at))
		if err != nil {
			t.Fatalf("TOTPCode: %v", err)
		}
		if got != want {
			t.Fatalf("code at T=%d = %s, want %s (RFC 6238 %s)", tc.unix, got, want, tc.eight)
		}
	}
}

func TestNewTOTPSecretIsUnpaddedUppercaseBase32(t *testing.T) {
	seen := map[string]bool{}
	for range 8 {
		secret, err := NewTOTPSecret()
		if err != nil {
			t.Fatal(err)
		}
		if strings.ContainsAny(secret, "=") || secret != strings.ToUpper(secret) {
			t.Fatalf("secret = %q, want unpadded uppercase base32", secret)
		}
		key, err := decodeTOTPSecret(secret)
		if err != nil || len(key) != totpSeedBytes {
			t.Fatalf("decode %q = %d bytes, %v", secret, len(key), err)
		}
		if seen[secret] {
			t.Fatalf("secret %q minted twice", secret)
		}
		seen[secret] = true
	}
}

func TestDecodeTOTPSecretAcceptsPastedForms(t *testing.T) {
	canonical, err := normalizeTOTPSecret(rfcSeed)
	if err != nil {
		t.Fatal(err)
	}
	if canonical != rfcSeed {
		t.Fatalf("normalize(%q) = %q", rfcSeed, canonical)
	}
	for _, form := range []string{
		strings.ToLower(rfcSeed),
		rfcSeed[:8] + " " + rfcSeed[8:],
		rfcSeed + "=",
	} {
		got, err := normalizeTOTPSecret(form)
		if err != nil {
			t.Fatalf("normalize(%q): %v", form, err)
		}
		if got != rfcSeed {
			t.Fatalf("normalize(%q) = %q, want %q", form, got, rfcSeed)
		}
	}
	if _, err := decodeTOTPSecret("not-base32!"); err == nil {
		t.Fatal("expected a base32 error")
	}
}

func TestTOTPVerifyWindowAndReplay(t *testing.T) {
	key, err := decodeTOTPSecret(rfcSeed)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1111111111, 0).UTC()
	cur := TOTPStep(now)

	for _, delta := range []int64{-1, 0, 1} {
		step, ok := totpVerify(key, hotp(key, cur+delta), now, 0)
		if !ok || step != cur+delta {
			t.Fatalf("step %+d: verify = %d, %v; want the step accepted", delta, step, ok)
		}
	}
	for _, delta := range []int64{-2, 2, 30} {
		if _, ok := totpVerify(key, hotp(key, cur+delta), now, 0); ok {
			t.Fatalf("step %+d accepted; the window is ±%d", delta, totpSkew)
		}
	}
	// Replay: the consumed step and everything below it are refused.
	if _, ok := totpVerify(key, hotp(key, cur), now, cur); ok {
		t.Fatal("a consumed code verified a second time")
	}
	if _, ok := totpVerify(key, hotp(key, cur-1), now, cur); ok {
		t.Fatal("a code below the consumed step verified")
	}
	if _, ok := totpVerify(key, hotp(key, cur+1), now, cur); !ok {
		t.Fatal("the next step must still verify after a consumption")
	}
	if _, ok := totpVerify(key, "000000", now, 0); ok {
		t.Fatal("a fixed wrong code verified")
	}
	// Grouped input is the form an authenticator app shows.
	code := hotp(key, cur)
	if _, ok := totpVerify(key, code[:3]+" "+code[3:], now, 0); !ok {
		t.Fatal("a space-grouped code must verify")
	}
}

func TestTOTPEnrollmentURI(t *testing.T) {
	uri := TOTPEnrollmentURI("geoah", rfcSeed)
	want := "otpauth://totp/Substrate:geoah?secret=" + rfcSeed +
		"&issuer=Substrate&algorithm=SHA1&digits=6&period=30"
	if uri != want {
		t.Fatalf("uri = %q, want %q", uri, want)
	}
}
