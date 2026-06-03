#!/usr/bin/env bash
# Thin wrapper so `make smoke` runs the Python e2e validation with a Python that
# exists. Uses the repo venv if present, else system python3.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PY=python3
exec "$PY" "$ROOT/deploy/smoke.py"
