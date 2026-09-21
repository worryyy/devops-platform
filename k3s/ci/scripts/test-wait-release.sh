#!/usr/bin/env sh
# test-wait-release.sh — fixture-driven tests for wait-for-release.sh verdicts.
set -eu

repo_root=$(git rev-parse --show-toplevel)
script="${repo_root}/k3s/ci/scripts/wait-for-release.sh"
fixtures="${repo_root}/k3s/ci/fixtures"

service_json='{"workload_kind":"Deployment","workload":"ecampus-academic","resource_name":"ecampus-academic","namespace":"app","wait_timeout":"10s","stable_service":"ecampus-academic","health_path":"/health","max_p95_seconds":0.8}'

assert_verdict() {
  expected="$1"
  deployment_json="$2"
  actual=$(SERVICE_JSON_FILE="$fixtures/service.json" DEPLOYMENT_JSON_FILE="$deployment_json" "$script" evaluate)
  if [ "$actual" != "$expected" ]; then
    echo "expected verdict $expected, got $actual ($deployment_json)" >&2
    exit 1
  fi
}

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT
printf '%s' "$service_json" > "$fixtures/service.json"

assert_verdict healthy "${fixtures}/deployment-healthy/deployment.json"

# A deployment stuck below its desired replicas stays pending and times out.
jq '.status.availableReplicas = 0' "${fixtures}/deployment-healthy/deployment.json" > "${tmpdir}/pending-deployment.json"
assert_verdict pending "${tmpdir}/pending-deployment.json"

# Digest assertions.
if SERVICE_JSON_FILE="$fixtures/service.json" \
  DEPLOYMENT_JSON_FILE="$fixtures/digest/deployment.json" \
  EXPECTED_DIGEST="sha256:2222222222222222222222222222222222222222222222222222222222222222" \
  "$script" evaluate-digest >/dev/null 2>&1; then
  :
else
  echo "expected deployment digest assertion to pass" >&2
  exit 1
fi
if SERVICE_JSON_FILE="$fixtures/service.json" \
  DEPLOYMENT_JSON_FILE="$fixtures/digest/deployment.json" \
  EXPECTED_DIGEST="sha256:9999999999999999999999999999999999999999999999999999999999999999" \
  "$script" evaluate-digest >/dev/null 2>&1; then
  echo "expected deployment digest mismatch to fail" >&2
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
if ! FIXTURE_MODE=1 SERVICE_JSON_FILE="$fixtures/service.json" \
  METRICS_JSON_FILE="$fixtures/metrics/pass.json" \
  METRICS_OBSERVATION_SECONDS=0 \
  "$script" verify-metrics >/dev/null 2>&1; then
  echo "expected healthy metrics to pass" >&2
  exit 1
fi
for metrics in insufficient error; do
  if FIXTURE_MODE=1 SERVICE_JSON_FILE="$fixtures/service.json" \
    METRICS_JSON_FILE="$fixtures/metrics/${metrics}.json" \
    METRICS_OBSERVATION_SECONDS=0 \
    "$script" verify-metrics >/dev/null 2>&1; then
    echo "expected ${metrics} metrics to fail" >&2
    exit 1
  fi
done

# Pending deployment that never becomes healthy times out in wait mode.
if FIXTURE_MODE=1 \
  KUBECTL_CLI=true \
  SERVICE_JSON_FILE="$fixtures/service.json" \
  DEPLOYMENT_JSON_FILE="${tmpdir}/pending-deployment.json" \
  EXPECTED_GIT_SHA="a1b2c3d4" \
  METRICS_POLL_SECONDS=1 \
  "$script" wait >/dev/null 2>&1; then
  echo "expected pending wait to time out" >&2
  exit 1
fi

rm -f "$fixtures/service.json"
echo "all wait-for-release fixture tests passed"
