#!/usr/bin/env bash

set -euo pipefail

BASE="${BASE:-http://localhost:8080}"
DURATION="${DURATION:-15}"
CONCURRENCY="${CONCURRENCY:-10}"

TARGET=$(docker compose ps -q app | head -n1)
if [ -z "$TARGET" ]; then
    echo "no running 'app' container found. run 'docker compose up -d' first." >&2
    exit 1
fi
echo "target container: $TARGET"

OK_FILE=$(mktemp)
FAIL_FILE=$(mktemp)
trap 'rm -f "$OK_FILE" "$FAIL_FILE"' EXIT
echo 0 > "$OK_FILE"
echo 0 > "$FAIL_FILE"

START=$(date +%s)
END=$((START + DURATION))

for i in $(seq 1 "$CONCURRENCY"); do
    (
        while [ "$(date +%s)" -lt "$END" ]; do
            if curl -fsS --max-time 5 "$BASE/healthz" >/dev/null 2>&1; then
                n=$(cat "$OK_FILE")
                echo $((n+1)) > "$OK_FILE"
            else
                n=$(cat "$FAIL_FILE")
                echo $((n+1)) > "$FAIL_FILE"
            fi
        done
    ) &
done

sleep 3
echo ""
echo "==> sending SIGTERM to $TARGET"
docker kill --signal=SIGTERM "$TARGET" >/dev/null
echo "==> SIGTERM sent at $(date +%T)"
echo ""

wait

OK=$(cat "$OK_FILE")
FAIL=$(cat "$FAIL_FILE")
TOTAL=$((OK + FAIL))

echo "=============================="
echo "total requests : $TOTAL"
echo "successful     : $OK"
echo "failed         : $FAIL"
echo "=============================="

if [ "$FAIL" -gt 0 ]; then
    echo "WARNING: $FAIL requests failed."
    echo "Hint: посмотри логи 'docker compose logs app' — увидишь,"
    echo "      что инстанс получил SIGTERM и закрылся после inflight."
    exit 2
fi

echo "OK: graceful shutdown без потерь."

