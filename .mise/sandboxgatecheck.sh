#!/usr/bin/env bash
#
# The sandbox-gate guard: the CI tasks still arm the confinement suites.
#
# Why it exists: internal/sandbox and internal/runner SKIP the cases that
# assert what a function body cannot do wherever the kernel offers no Landlock
# or no seccomp, and only SUBSTRATE_TEST_REQUIRE_SANDBOX turns those skips into
# failures. A disarmed gate is a GREEN build, so nothing else here would report
# either of the two edits that disarm it: dropping the `env` line from `ci:go`
# or `ci:race`, and turning either task back into `depends = [...]`, which mise
# runs with an environment of its own so the variable never reaches the test
# binary.
#
# It reads `mise task info` rather than the TOML, so it holds what mise will
# actually do with the task.
#
# No `-e`: both tasks are reported, not just the first to break.
set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || exit 2

fail=0

flag() {
  fail=1
  printf 'lint:sandboxgate: %s\n' "$*" >&2
}

for task in ci:go ci:race; do
  info="$(mise task info "$task" 2>/dev/null)"
  if [ -z "$info" ]; then
    flag "no ${task} task, so this guard holds nothing"
    continue
  fi
  case "$info" in
    *SUBSTRATE_TEST_REQUIRE_SANDBOX=1*) ;;
    *) flag "${task} does not set SUBSTRATE_TEST_REQUIRE_SANDBOX=1: its confinement cases would skip a kernel that lost Landlock or seccomp instead of failing on it" ;;
  esac
  case "$info" in
    *"mise run test:"*) ;;
    *) flag "${task} does not run a test task through 'run = \"mise run test:...\"': a depends edge gets its own environment and would drop SUBSTRATE_TEST_REQUIRE_SANDBOX" ;;
  esac
done

exit "$fail"
