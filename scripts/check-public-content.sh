#!/usr/bin/env bash

set -euo pipefail

status=0

scan() {
  local description="$1"
  local pattern="$2"
  shift 2

  local matches
  matches="$(mktemp)"
  if rg -l --hidden --pcre2 \
    --glob '!.git/**' \
    --glob '!docs/task/**' \
    --glob '!docs/private/**' \
    --glob '!scripts/check-public-content.sh' \
    "$@" \
    -- "$pattern" . >"$matches"; then
    echo "security scan failed: $description found in:"
    sed 's#^./#  #' "$matches"
    status=1
  fi
  rm "$matches"
}

scan 'private key material' \
  '-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----'

scan 'known access-token format' \
  '(?:github_pat_[A-Za-z0-9_]{20,}|gh[pousr]_[A-Za-z0-9_]{30,}|AKIA[0-9A-Z]{16}|ASIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{30,}|xox[baprs]-[0-9A-Za-z-]{20,}|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,})'

scan 'hard-coded credential assignment' \
  "(?i)(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|client[_-]?secret|private[_-]?key)[[:space:]]*[:=][[:space:]]*[\"'][^\"']{8,}[\"']"

scan 'hard-coded unquoted credential in configuration' \
  '(?i)(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|client[_-]?secret|private[_-]?key)[[:space:]]*:[[:space:]]*[A-Za-z0-9/+_.-]{12,}[[:space:]]*$' \
  --glob '*.yaml' \
  --glob '*.yml' \
  --glob '*.json'

scan 'private network address outside sanitizer tests' \
  '(?:\b10(?:\.[0-9]{1,3}){3}\b|\b192\.168(?:\.[0-9]{1,3}){2}\b|\b172\.(?:1[6-9]|2[0-9]|3[01])(?:\.[0-9]{1,3}){2}\b)' \
  --glob '!internal/snapshot/snapshot.go' \
  --glob '!internal/snapshot/snapshot_test.go'

scan 'internal DNS suffix outside sanitizer tests' \
  '(?i)(?:\.homelab\b|\.svc\.cluster\.local\b)' \
  --glob '!internal/snapshot/snapshot.go' \
  --glob '!internal/snapshot/snapshot_test.go'

scan 'possible account or project identifier' \
  '(?:\b[0-9a-fA-F]{32}\b|\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}\b)' \
  --glob '!**/*_test.go'

exit "$status"
