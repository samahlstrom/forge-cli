#!/usr/bin/env bash
# Measure script for forge refine — outputs JSON metrics about build health.
# This file is tamper-proof: the agent must not edit it.
set -euo pipefail

start_ns=$(date +%s%N 2>/dev/null || python3 -c "import time; print(int(time.time()*1e9))")

# 1. Build
build_ok=0
build_output=$(go build ./... 2>&1) && build_ok=1
build_end_ns=$(date +%s%N 2>/dev/null || python3 -c "import time; print(int(time.time()*1e9))")
build_ms=$(( (build_end_ns - start_ns) / 1000000 ))

# 2. Vet
vet_ok=0
vet_output=$(go vet ./... 2>&1) && vet_ok=1

# 3. Tests
test_ok=0
test_total=0
test_passed=0
test_failed=0
test_output=$(go test ./... -count=1 -timeout 60s 2>&1) && test_ok=1

# Parse test counts from output
test_passed=$(echo "$test_output" | grep -c "^ok" || true)
test_failed=$(echo "$test_output" | grep -c "^FAIL" || true)
test_total=$((test_passed + test_failed))

# Pass rate
if [ "$test_total" -gt 0 ]; then
  pass_rate=$(echo "scale=4; $test_passed / $test_total" | bc)
else
  pass_rate="1.0000"
fi

# 4. Binary size
binary_size=0
if go build -o /tmp/forge-measure-bin . 2>/dev/null; then
  binary_size=$(stat -f%z /tmp/forge-measure-bin 2>/dev/null || stat -c%s /tmp/forge-measure-bin 2>/dev/null || echo 0)
  rm -f /tmp/forge-measure-bin
fi

end_ns=$(date +%s%N 2>/dev/null || python3 -c "import time; print(int(time.time()*1e9))")
total_ms=$(( (end_ns - start_ns) / 1000000 ))

# Output JSON
cat <<EOJSON
{"build_ok": ${build_ok}, "vet_ok": ${vet_ok}, "test_pass_rate": ${pass_rate}, "test_passed": ${test_passed}, "test_failed": ${test_failed}, "build_ms": ${build_ms}, "binary_bytes": ${binary_size}, "total_ms": ${total_ms}}
EOJSON
