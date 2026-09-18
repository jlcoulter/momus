#!/usr/bin/env bash
#
# Validate all FHIR resources that momus generates against the HCPD IG using
# the official FHIR validator (validator_cli.jar).
#
# Flow:
#   1. Generate a test plan from a package (.tgz) via `momus coverage plan`.
#   2. Extract the generated seed resources from the plan's dataset.
#   3. Validate every resource against its declared profile (via the IG) with
#      the R4 validator, terminology server disabled.
#   4. Emit a per-resource pass/fail summary plus a categorized error report.
#
# Usage:
#   validate-generated.sh [PACKAGE.tgz] [MOMUS_BIN] [VALIDATOR_JAR]
#
# Defaults:
#   PACKAGE      = ~/Downloads/hcpd.tgz
#   MOMUS_BIN    = (built to a temp location)
#   VALIDATOR_JAR= ~/Downloads/validator_cli.jar

set -euo pipefail

PKG="${1:-$HOME/Downloads/hcpd.tgz}"
MOMUS_BIN="${2:-}"
VALIDATOR_JAR="${3:-$HOME/Downloads/validator_cli.jar}"
FHIR_VERSION=4.0.1

# Directory where the package cache (deps + IG) and extracted resources live.
# (Use a repo-local path so logs are readable; set WORK to override.)
WORK="${WORK:-/home/jc/git/momus/.validate-work}"
PACKAGE_CACHE="$WORK/packages"
RESOURCES_DIR="$WORK/resources"
VALIDATE_LOG="$WORK/all-validate.log"

# ---------------------------------------------------------------------------
# 0. Locate/install the validator's package cache (IG + deps) so profiles resolve.
# ---------------------------------------------------------------------------
ensure_packages() {
  echo "[1/5] ensuring validator package cache..."
  local cache="${FHIR_PACKAGE_CACHE:-$HOME/.fhir/packages}"
  local deps_dir="${MOMUS_DEPS_DIR:-}"
  if [ -z "$deps_dir" ] && [ -d "$HOME/git/fhir-registry" ]; then
    deps_dir="$HOME/git/fhir-registry"
  fi
  if [ -n "$deps_dir" ] && [ -d "$deps_dir" ]; then
    # momus already resolved the deps; install any missing ones into the cache.
    for tgz in "$deps_dir"/*.tgz; do
      [ -e "$tgz" ] || continue
      local name
      name=$(tar xzf "$tgz" -O package/package.json 2>/dev/null | python3 -c "import sys,json;d=json.load(sys.stdin);print(d['name']+'#'+d['version'])" 2>/dev/null || true)
      if [ -n "$name" ] && [ ! -d "$cache/$name" ]; then
        mkdir -p "$cache/$name/package"
        tar xzf "$tgz" -C "$cache/$name/package" --strip-components=1 2>/dev/null
        echo "installed $name"
      fi
    done
  fi
  # Install the IG itself.
  local igname
  igname=$(tar xzf "$PKG" -O package/package.json | python3 -c "import sys,json;d=json.load(sys.stdin);print(d['name']+'#'+d['version'])")
  if [ ! -d "$cache/$igname" ]; then
    mkdir -p "$cache/$igname/package"
    tar xzf "$PKG" -C "$cache/$igname/package" --strip-components=1
    echo "installed $igname"
  fi
  echo "IG package: $igname (cache: $cache)"
}

# ---------------------------------------------------------------------------
# 1. Build momus and generate the test plan.
# ---------------------------------------------------------------------------
generate() {
  echo "[2/5] generating test plan..."
  if [ -z "$MOMUS_BIN" ]; then
    MOMUS_BIN="$WORK/momus"
    (cd "$(git rev-parse --show-toplevel 2>/dev/null || echo .)" && go build -o "$MOMUS_BIN" ./cmd/momus)
  fi
  mkdir -p "$WORK/deps"
  # momus must resolve the IG's dependencies to build the full registry. The
  # deps live in a local directory (e.g. git/fhir-registry/*.tgz); if none is
  # given, fall back to the validator package cache, then to the download dir.
  local deps="${MOMUS_DEPS_DIR:-}"
  if [ -z "$deps" ] && [ -d "$HOME/git/fhir-registry" ]; then
    deps="$HOME/git/fhir-registry"
  fi
  local args=("--download-dir" "$WORK/deps" "--output" "$WORK/plan.json")
  if [ -n "$deps" ]; then
    args+=("--deps-dir" "$deps")
  fi
  "$MOMUS_BIN" coverage plan "$PKG" "${args[@]}"
}

# ---------------------------------------------------------------------------
# 2. Extract seed resources from the plan's dataset into per-resource files.
# ---------------------------------------------------------------------------
extract() {
  echo "[3/5] extracting seed resources..."
  mkdir -p "$RESOURCES_DIR"
  rm -f "$RESOURCES_DIR"/*.json
  python3 - "$WORK/plan.json" "$RESOURCES_DIR" <<'PY'
import json, os, sys
plan, outdir = sys.argv[1], sys.argv[2]
d = json.load(open(plan))
res = d['dataset']['resources']
for k, inst in res.items():
    body = inst['resource']
    rt = inst['resourceType']
    fn = os.path.join(outdir, f"{rt}__{k}.json")
    with open(fn, 'w') as f:
        json.dump(body, f, indent=2)
print(f"extracted {len(res)} resources")
PY
}

# ---------------------------------------------------------------------------
# 3. Validate every resource against the IG (R4, no terminology server).
# ---------------------------------------------------------------------------
validate() {
  echo "[4/5] validating resources (this takes a while)..."
  local ig
  ig=$(tar xzf "$PKG" -O package/package.json | python3 -c "import sys,json;d=json.load(sys.stdin);print(d['name'])")
  java -jar "$VALIDATOR_JAR" \
    -version "$FHIR_VERSION" \
    -tx n/a \
    -ig "$ig" \
    "$RESOURCES_DIR" > "$VALIDATE_LOG" 2>&1 || true
  echo "validation log: $VALIDATE_LOG"
}

# ---------------------------------------------------------------------------
# 4. Summarize per-resource pass/fail and a categorized error report.
# ---------------------------------------------------------------------------
summarize() {
  echo "[5/5] summarizing results..."
  python3 - "$VALIDATE_LOG" "$RESOURCES_DIR" "$WORK/summary.txt" <<'PY'
import re, collections, os, sys
log, resdir, out = sys.argv[1], sys.argv[2], sys.argv[3]
errors = collections.Counter()
failed = collections.Counter()   # file -> error count
cur = None
for line in open(log):
    s = line.rstrip()
    if s.startswith('-- ') and ' ----' in s:
        cur = s.split('-- ')[1].split(' ----')[0]
        if '/' in cur:
            cur = cur.split('/')[-1]
        failed.setdefault(cur, 0)
        continue
    t = s.strip()
    if cur is not None and t.startswith('Error @'):
        failed[cur] += 1
        m = re.match(r'Error @ (.+?) \(line .*?\): (.*)$', t)
        if m:
            errors[(m.group(1).strip(), m.group(2).strip())] += 1

files = sorted(f for f in os.listdir(resdir) if f.endswith('.json'))
passes = [f for f in files if failed.get(f, 0) == 0]
fails = [f for f in files if failed.get(f, 0) > 0]
lines = []
lines.append(f"Total resources: {len(files)}")
lines.append(f"  passed: {len(passes)}")
lines.append(f"  failed: {len(fails)}")
lines.append(f"  distinct error patterns: {len(errors)}")
lines.append("\n=== Top 60 error patterns ===")
for (path, msg), c in errors.most_common(60):
    lines.append(f"{c:5d}  {path}\n          :: {msg[:95]}")
lines.append("\n=== Failing resources (by error count) ===")
for f, c in failed.most_common(40):
    lines.append(f"{c:5d}  {f}")
open(out, 'w').write("\n".join(lines) + "\n")
print(f"summary written to {out}")
PY
}

ensure_packages
generate
extract
validate
summarize
echo "HARNESS_COMPLETE"
