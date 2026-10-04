#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
minimum="${MIN_EFFICACY:-0}"
tools="$(mktemp -d)"
trap 'rm -r "$tools"' EXIT
GOBIN="$tools" go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0
report="$(GOFLAGS="-exec=$PWD/tests/limit-memory.sh" "$tools/gremlins" unleash --workers 1 --timeout-coefficient 10 "$@" . 2>&1)"
echo "$report"
efficacy="$(sed -n 's/^Test efficacy: \([0-9.]*\)%$/\1/p' <<<"$report")"
if [ -z "$efficacy" ]; then
  echo "mutation: no efficacy in the gremlins report" >&2
  exit 1
fi
if ! awk -v got="$efficacy" -v want="$minimum" 'BEGIN { exit !(got >= want) }'; then
  echo "mutation: efficacy ${efficacy}% is below ${minimum}%" >&2
  exit 1
fi
