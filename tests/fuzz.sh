#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
fuzztime="${FUZZTIME:-30s}"
for target in $(go test -list '^Fuzz' . | grep '^Fuzz'); do
  echo "fuzzing $target for $fuzztime"
  go test -run '^$' -fuzz "^${target}\$" -fuzztime "$fuzztime" .
done
