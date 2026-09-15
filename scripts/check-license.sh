#!/usr/bin/env bash
# Copyright 2026 The Kube SRE MCP Authors
# SPDX-License-Identifier: Apache-2.0
#
# Validation only: does not rewrite source files or LICENSE.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ ! -f LICENSE ]]; then
  echo "license-check: LICENSE is missing at repository root." >&2
  exit 1
fi

tmpfile="$(mktemp)"
cleanup() { rm -f "$tmpfile"; }
trap cleanup EXIT

url="https://www.apache.org/licenses/LICENSE-2.0.txt"
if ! curl -fsSL "$url" -o "$tmpfile"; then
  echo "license-check: failed to download ${url}." >&2
  echo "license-check: network access is required; LICENSE was not modified." >&2
  exit 1
fi

if ! cmp -s LICENSE "$tmpfile"; then
  echo "license-check: LICENSE does not match the official Apache License 2.0 text from ${url}." >&2
  exit 1
fi

if ! grep -q "Apache License" LICENSE || ! grep -q "Version 2.0, January 2004" LICENSE; then
  echo "license-check: LICENSE is missing expected Apache License 2.0 markers." >&2
  exit 1
fi

missing=0
while IFS= read -r -d '' f; do
  if ! grep -q "SPDX-License-Identifier: Apache-2.0" "$f"; then
    echo "license-check: missing SPDX-License-Identifier: Apache-2.0 in ${f}" >&2
    missing=1
  fi
done < <(find cmd internal -name '*.go' -print0 | sort -z)

expected=(
  install/install.sh
  scripts/check-license.sh
  Makefile
  Dockerfile
  go.mod
)
for f in "${expected[@]}"; do
  if [[ ! -f "$f" ]]; then
    echo "license-check: expected project file missing: ${f}" >&2
    missing=1
    continue
  fi
  if ! grep -q "SPDX-License-Identifier: Apache-2.0" "$f"; then
    echo "license-check: missing SPDX-License-Identifier: Apache-2.0 in ${f}" >&2
    missing=1
  fi
done

if [[ ! -d .github/workflows ]]; then
  echo "license-check: expected project directory missing: .github/workflows" >&2
  missing=1
else
  shopt -s nullglob
  workflows=(.github/workflows/*.yml)
  shopt -u nullglob
  if [[ ${#workflows[@]} -eq 0 ]]; then
    echo "license-check: no GitHub Actions workflows found under .github/workflows" >&2
    missing=1
  fi
  for f in "${workflows[@]}"; do
    if ! grep -q "SPDX-License-Identifier: Apache-2.0" "$f"; then
      echo "license-check: missing SPDX-License-Identifier: Apache-2.0 in ${f}" >&2
      missing=1
    fi
  done
fi

if [[ "$missing" -ne 0 ]]; then
  echo "license-check: SPDX validation failed for project-owned files." >&2
  exit 1
fi

# Detect false ASF/CNCF affiliation in project docs (not LICENSE or this validator).
affiliation=0
while IFS= read -r -d '' f; do
  case "$f" in
    ./LICENSE|./LICENSE.*|./scripts/check-license.sh) continue ;;
  esac
  if grep -Eiq 'Apache Software Foundation Project|Official Apache Project|Copyright The Apache Software Foundation' "$f"; then
    echo "license-check: suspicious Apache Software Foundation affiliation claim in ${f}" >&2
    affiliation=1
  fi
  if grep -Eiq 'CNCF Project|CNCF Sandbox|CNCF Certified|Hosted by CNCF|Official CNCF|Copyright CNCF' "$f"; then
    echo "license-check: suspicious CNCF affiliation/status claim in ${f}" >&2
    affiliation=1
  fi
done < <(find . -type f \
  \( -name '*.md' -o -name '*.go' -o -name '*.yml' -o -name '*.yaml' -o -name 'NOTICE' \) \
  ! -path './.git/*' ! -path './vendor/*' ! -path './bin/*' -print0)

if [[ "$affiliation" -ne 0 ]]; then
  echo "license-check: false affiliation claims detected." >&2
  exit 1
fi

echo "license-check: LICENSE matches ${url}"
echo "license-check: SPDX markers present on project-owned files"
echo "license-check: no false ASF/CNCF affiliation claims found"
