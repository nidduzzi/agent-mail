#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
fuzztime="${FUZZTIME:-30s}"
targets="${*:-$(go test -list '^Fuzz' . | grep '^Fuzz')}"
for target in $targets; do
  echo "fuzzing $target for $fuzztime"
  go test -run '^$' -fuzz "^${target}\$" -fuzztime "$fuzztime" .
done
