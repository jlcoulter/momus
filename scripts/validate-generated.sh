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

# Locate the repo root from this script's own path so the harness works
# regardless of where the sibling repos live or how it is invoked.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

PKG="${1:-$HOME/Downloads/hcpd.tgz}"
MOMUS_BIN="${2:-}"
VALIDATOR_JAR="${3:-$HOME/Downloads/validator_cli.jar}"
FHIR_VERSION=4.0.1

# Directory where the package cache (deps + IG) and extracted resources live.
# (Use a repo-local path so logs are readable; set WORK to override.)
WORK="${WORK:-$REPO_ROOT/.validate-work}"
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
  if [ -z "$deps_dir" ] && [ -d "$HOME/Downloads/.momus/packages" ]; then
    deps_dir="$HOME/Downloads/.momus/packages"
  fi
  # Install the IG itself into the validator cache (overwrite so the cache is
  # always in sync with the package being tested, never a stale prior build).
  local igname
  igname=$(tar xzf "$PKG" -O package/package.json | python3 -c "import sys,json;d=json.load(sys.stdin);print(d['name']+'#'+d['version'])")
  mkdir -p "$cache/$igname/package"
  tar xzf "$PKG" -C "$cache/$igname/package" --strip-components=1
  echo "IG package: $igname (cache: $cache)"
}

# ---------------------------------------------------------------------------
# 0b. Sync the validator package cache with the dependencies momus resolved so
#     the validator validates against the SAME package versions momus generated
#     against. Floating dependency references (e.g. "current"/"latest") resolve
#     differently between the public registries and a manually-populated cache,
#     so any floating cache entry is overwritten with momus's resolved archive
#     rather than left stale.
# ---------------------------------------------------------------------------
sync_packages() {
  local cache="${FHIR_PACKAGE_CACHE:-$HOME/.fhir/packages}"
  # momus writes every resolved dependency archive to WORK/deps.
  local deps_dir="$WORK/deps"
  if [ -d "$deps_dir" ]; then
    for tgz in "$deps_dir"/*.tgz; do
      [ -e "$tgz" ] || continue
      local meta name version
      meta=$(tar xzf "$tgz" -O package/package.json 2>/dev/null)
      name=$(printf '%s' "$meta" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('name',''))" 2>/dev/null || true)
      version=$(printf '%s' "$meta" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('version',''))" 2>/dev/null || true)
      [ -n "$name" ] || continue
      # Install under name#version (the concrete resolved version).
      mkdir -p "$cache/$name#$version/package"
      tar xzf "$tgz" -C "$cache/$name#$version/package" --strip-components=1
      # Install the same content under any floating alias ("current"/"latest")
      # so the validator resolves the floating ref to the exact version momus
      # generated against, never a stale different build.
      for alias in current latest; do
        rm -rf "$cache/$name#$alias"
        mkdir -p "$cache/$name#$alias/package"
        tar xzf "$tgz" -C "$cache/$name#$alias/package" --strip-components=1
      done
      echo "synced $name#$version"
    done
  fi
}

# ---------------------------------------------------------------------------
# 1. Build momus and generate the test plan.
# ---------------------------------------------------------------------------
generate() {
  echo "[2/5] generating test plan..."
  if [ -z "$MOMUS_BIN" ]; then
    MOMUS_BIN="$WORK/momus"
    (cd "$REPO_ROOT" && go build -o "$MOMUS_BIN" ./cmd/momus)
  fi
  mkdir -p "$WORK/deps"
  # momus must resolve the IG's dependencies to build the full registry. The
  # deps live in a local directory (e.g. git/fhir-registry/*.tgz); if none is
  # given, fall back to the validator package cache, then to the download dir.
  local deps="${MOMUS_DEPS_DIR:-}"
  if [ -z "$deps" ] && [ -d "$HOME/Downloads/.momus/packages" ]; then
    deps="$HOME/Downloads/.momus/packages"
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
sync_packages
extract
validate
summarize
echo "HARNESS_COMPLETE"
