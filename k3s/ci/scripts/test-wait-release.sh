#!/usr/bin/env sh
# test-wait-release.sh — fixture-driven tests for wait-for-release.sh verdicts.
set -eu

repo_root=$(git rev-parse --show-toplevel)
script="${repo_root}/k3s/ci/scripts/wait-for-release.sh"
fixtures="${repo_root}/k3s/ci/fixtures"

assert_verdict() {
  expected="$1"
  shift
  actual=$(ROLLOUT_JSON_FILE="$1" ANALYSISRUN_JSON_FILE="${2:-}" DEPLOYMENT_JSON_FILE="${3:-}" "$script" evaluate)
  if [ "$actual" != "$expected" ]; then
    echo "expected verdict $expected, got $actual ($1)" >&2
    exit 1
  fi
}

assert_verdict healthy "${fixtures}/healthy/rollout.json" "${fixtures}/healthy/analysisrun.json"
assert_verdict failed "${fixtures}/failed/rollout.json" "${fixtures}/failed/analysisrun.json"
assert_verdict error "${fixtures}/error/rollout.json" "${fixtures}/error/analysisrun.json"
assert_verdict inconclusive "${fixtures}/inconclusive/rollout.json" "${fixtures}/inconclusive/analysisrun.json"
assert_verdict pending "${fixtures}/pending/rollout.json" "${fixtures}/pending/analysisrun.json"
assert_verdict healthy "${fixtures}/deployment-healthy/rollout.json" "" "${fixtures}/deployment-healthy/rollout.json"

# Digest assertions.
if SERVICE_JSON_FILE="$fixtures/rollback/service-canary.json" \
  ROLLOUT_JSON_FILE="$fixtures/digest/rollout.json" \
  EXPECTED_DIGEST="sha256:1111111111111111111111111111111111111111111111111111111111111111" \
  "$script" evaluate-digest >/dev/null 2>&1; then
  :
else
  echo "expected rollout digest assertion to pass" >&2
  exit 1
fi
if SERVICE_JSON_FILE="$fixtures/rollback/service-canary.json" \
  ROLLOUT_JSON_FILE="$fixtures/digest/rollout.json" \
  EXPECTED_DIGEST="sha256:9999999999999999999999999999999999999999999999999999999999999999" \
  "$script" evaluate-digest >/dev/null 2>&1; then
  echo "expected rollout digest mismatch to fail" >&2
  exit 1
fi
if ! SERVICE_JSON_FILE="$fixtures/rollback/service-deployment.json" \
  DEPLOYMENT_JSON_FILE="$fixtures/digest/deployment.json" \
  EXPECTED_DIGEST="sha256:2222222222222222222222222222222222222222222222222222222222222222" \
  "$script" evaluate-digest >/dev/null 2>&1; then
  echo "expected deployment digest assertion to pass" >&2
  exit 1
fi

# Argo CD sync/health assertions.
if ! ARGOCD_APPLICATION_JSON_FILE="$fixtures/argocd/synced.json" \
  CONFIG_REVISION="abcd1234abcd1234abcd1234abcd1234abcd1234" \
  "$script" evaluate-argocd >/dev/null 2>&1; then
  echo "expected synced Argo CD application to pass" >&2
  exit 1
fi
if ARGOCD_APPLICATION_JSON_FILE="$fixtures/argocd/unsynced.json" \
  CONFIG_REVISION="abcd1234abcd1234abcd1234abcd1234abcd1234" \
  "$script" evaluate-argocd >/dev/null 2>&1; then
  echo "expected unsynced Argo CD application to fail" >&2
  exit 1
fi

# Post-release metrics verification.
service_json="$fixtures/rollback/service-canary.json"
if ! FIXTURE_MODE=1 SERVICE_JSON_FILE="$service_json" \
  METRICS_JSON_FILE="$fixtures/metrics/pass.json" \
  METRICS_OBSERVATION_SECONDS=1 \
  "$script" verify-metrics >/dev/null 2>&1; then
  echo "expected healthy metrics to pass" >&2
  exit 1
fi
for metrics in insufficient error; do
  if FIXTURE_MODE=1 SERVICE_JSON_FILE="$service_json" \
    METRICS_JSON_FILE="$fixtures/metrics/${metrics}.json" \
    METRICS_OBSERVATION_SECONDS=1 \
    "$script" verify-metrics >/dev/null 2>&1; then
    echo "expected ${metrics} metrics to fail" >&2
    exit 1
  fi
done

# Pending release that never finishes times out in wait mode.
fixture_dir=$(mktemp -d)
trap 'rm -rf "$fixture_dir"' EXIT
cat > "$fixture_dir/service.json" <<'EOF'
{"workload_kind":"Rollout","rollout":"ecampus-academic","resource_name":"ecampus-academic","namespace":"app","wait_timeout":"10s","effective_profile":"standard-canary","stable_service":"ecampus-academic","health_path":"/health"}
EOF
if FIXTURE_MODE=1 \
  ROLLOUTS_CLI=true \
  SERVICE_JSON_FILE="$fixture_dir/service.json" \
  ROLLOUT_JSON_FILE="${fixtures}/pending/rollout.json" \
  EXPECTED_GIT_SHA="a1b2c3d4" \
  "$script" wait >/dev/null 2>&1; then
  echo "expected pending wait to time out" >&2
  exit 1
fi

# Inconclusive verdict with inconclusive_timeout 0s aborts immediately.
cat > "$fixture_dir/service.json" <<'EOF'
{"workload_kind":"Rollout","rollout":"ecampus-topic","resource_name":"ecampus-topic","namespace":"app","wait_timeout":"60s","effective_profile":"critical-canary","stable_service":"ecampus-topic","health_path":"/health","analysis":{"inconclusive_timeout":"0s"}}
EOF
if FIXTURE_MODE=1 \
  ROLLOUTS_CLI=true \
  SERVICE_JSON_FILE="$fixture_dir/service.json" \
  ROLLOUT_JSON_FILE="${fixtures}/inconclusive/rollout.json" \
  ANALYSISRUN_JSON_FILE="${fixtures}/inconclusive/analysisrun.json" \
  EXPECTED_GIT_SHA="a1b2c3d4" \
  "$script" wait >"$fixture_dir/inconclusive.out" 2>&1; then
  echo "expected inconclusive with 0s timeout to abort immediately" >&2
  exit 1
fi
grep -q 'aborts immediately' "$fixture_dir/inconclusive.out"

# Error verdict pauses progression and only aborts after the configured hold.
cat > "$fixture_dir/service.json" <<'EOF'
{"workload_kind":"Rollout","rollout":"ecampus-topic","resource_name":"ecampus-topic","namespace":"app","wait_timeout":"60s","effective_profile":"critical-canary","stable_service":"ecampus-topic","health_path":"/health","analysis":{"inconclusive_timeout":"5s"}}
EOF
if FIXTURE_MODE=1 \
  ROLLOUTS_CLI=true \
  SERVICE_JSON_FILE="$fixture_dir/service.json" \
  ROLLOUT_JSON_FILE="${fixtures}/error/rollout.json" \
  ANALYSISRUN_JSON_FILE="${fixtures}/error/analysisrun.json" \
  EXPECTED_GIT_SHA="a1b2c3d4" \
  "$script" wait >"$fixture_dir/error.out" 2>&1; then
  echo "expected error hold to abort after inconclusive_timeout" >&2
  exit 1
fi
grep -q 'held for 5 seconds' "$fixture_dir/error.out"

# Approval timeout: marker never appears.
marker="$fixture_dir/approved.marker"
rm -f "$marker"
if APPROVAL_MARKER="$marker" APPROVAL_TIMEOUT=5 "$script" approval-wait >/dev/null 2>&1; then
  echo "expected approval wait to time out" >&2
  exit 1
fi
APPROVAL_MARKER="$marker" APPROVAL_TIMEOUT=15 "$script" approval-wait >/dev/null 2>&1 || true

# Approval accepted within the deadline.
touch "$marker"
verdict=$(APPROVAL_MARKER="$marker" APPROVAL_TIMEOUT=10 "$script" approval-wait)
if [ "$verdict" != "approved" ]; then
  echo "expected approved verdict, got $verdict" >&2
  exit 1
fi

echo "all wait-for-release fixture tests passed"
