package observed

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// fusedInstruction is a fused multiply-add of the arm64 assembler, where the
// compiler's -S output names it, with the line it compiled.
var fusedInstruction = regexp.MustCompile(`\(([^()]*\.go):(\d+)\)\t(FN?M(?:ADD|SUB)[DS])\t`)

// THE ARITHMETIC IS THE SAME ON EVERY ARCHITECTURE (#1371). Go may fuse a
// multiply and an add into one instruction on a machine that has one, arm64
// among them, and does not on amd64; the fused form rounds once where the
// other rounds twice, so a rollup's moment, a time-weighted sum or a quantile
// would come out one bit apart between hosts. Every product is rounded on its
// own before its add, by an explicit float64 conversion, which the language
// keeps from fusing. This compiles the package for linux/arm64 and fails on
// every fused instruction left, by its line.
func TestNoArithmeticHereIsFusedOnArm64(t *testing.T) {
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("the guard compiles the package with the go command: %v", err)
	}
	cmd := exec.Command(gobin, "build", "-gcflags=github.com/geoah/substrate/internal/observed=-S", "-o", os.DevNull, ".")
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("compile the package for linux/arm64: %v\n%s", err, out)
	}
	if !regexp.MustCompile(`\tFMULD\t`).Match(out) {
		t.Fatal("the compiler's -S output holds no FMULD: the guard reads nothing")
	}
	for _, m := range fusedInstruction.FindAllSubmatch(out, -1) {
		t.Errorf("%s:%s compiles to %s on arm64: round the product on its own, float64(a*b) + c", filepath.Base(string(m[1])), m[2], m[3])
	}
}
