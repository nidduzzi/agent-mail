#!/usr/bin/env bash
ulimit -v "${TEST_MEMORY_KB:-2097152}"
exec "$@"
