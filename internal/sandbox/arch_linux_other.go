//go:build linux && !amd64 && !arm64

package sandbox

// The image ships linux/amd64 and linux/arm64, and a seccomp filter is written
// against ONE architecture's syscall numbering: getting that wrong does not
// fail loudly, it silently refuses or permits the wrong calls. So any other
// Linux architecture gets no syscall filter at all rather than a guess: zero
// here makes buildFilter refuse, applySeccomp reports the layer unavailable,
// and Landlock (which is numbering-independent) still applies.
const auditArch = 0

const (
	sysSocket = 0
	sysClone3 = 435
)

func archDenied() []denial { return nil }
