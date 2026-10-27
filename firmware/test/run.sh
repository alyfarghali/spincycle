#!/bin/sh
set -e
cd "$(dirname "$0")/.."
out=$(mktemp -d)
cc() { command cc -std=c11 -Wall -Wextra -Werror -I components/sensing/include -I components/connectivity/include "$@" -lm; }
cc test/test_rms.c components/sensing/rms.c -o "$out/rms" && echo "== rms" && "$out/rms"
cc test/test_telemetry.c components/connectivity/telemetry.c -o "$out/telemetry" && echo "== telemetry" && "$out/telemetry"
cc test/test_backoff.c components/connectivity/backoff.c -o "$out/backoff" && echo "== backoff" && "$out/backoff"
echo "all host tests passed"
