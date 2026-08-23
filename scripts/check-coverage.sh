#!/usr/bin/env bash
# Coverage gate for CI. Computes exclusion-aware statement coverage (the 
# excluded paths are deliberately hard to unit-test or non-executable) and fails
# the build when it falls below the required threshold.
#
# Exclusions:
#   cmd/momus                    CLI wiring / orchestration, not unit-testable logic
#   internal/fhir/bulk/synthesize.go  large unexported synthesis helpers, low ROI
#   internal/fhir/terminology        package doc only, no executable code
#
# Usage: check-coverage.sh [threshold]  (default 95)
set -euo pipefail

threshold="${1:-95}"

profile="$(mktemp)"
trap 'rm -f "$profile"' EXIT

# Generate a coverage profile across every package.
go test -coverprofile="$profile" ./... >/dev/null

# Compute coverage excluding the documented exclusions. The raw go cover
# profile is "mode: set" followed by "file:start,end numstmts count" lines.
awk -v threshold="$threshold" '
  /^mode:/ { next }
  $1 ~ /cmd\/momus/ { next }
  $1 ~ /synthesize\.go/ { next }
  $1 ~ /terminology\/doc\.go/ { next }
  { total += $2; if ($3 > 0) covered += $2 }
  END {
    pct = (covered * 100.0) / total
    printf "Coverage (excl. cmd/, synthesize.go, terminology): %.1f%% (%d/%d statements)\n", pct, covered, total
    if (pct < threshold) {
      printf "FAIL: coverage %.1f%% is below the required %.1f%%\n", pct, threshold
      exit 1
    }
  }
' "$profile"
