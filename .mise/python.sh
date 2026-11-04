#!/usr/bin/env bash
#
# Runs Python in the tree's one locked environment (.mise/python): every
# script here that imports more than the standard library, and every one
# that needs a Python newer than a Mac's own, runs through this and nothing
# else, so the release, the checks and a laptop read YAML with the same
# PyYAML.
#
# --locked: a lock that no longer matches .mise/python/pyproject.toml is
# refused, never resolved again, so what runs is what was reviewed. The
# environment is .mise/python/.venv, made by the first run and kept; its
# Python is one uv finds that the project allows, else one uv fetches.
#
#   .mise/python.sh <script> [args…]
#   .mise/python.sh - [args…] <<'PY' … PY
set -euo pipefail

exec uv run --quiet --locked --project "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/python" python "$@"
