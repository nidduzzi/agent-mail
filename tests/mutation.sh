#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 unleash --workers 1 --timeout-coefficient 10 "$@" .
