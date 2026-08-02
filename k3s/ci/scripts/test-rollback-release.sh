#!/usr/bin/env sh
# test-rollback-release.sh — fixture-driven tests for rollback-release.sh.
set -eu

repo_root=$(git rev-parse --show-toplevel)
script="${repo_root}/k3s/ci/scripts/rollback-release.sh"
fixtures="${repo_root}/k3s/ci/fixtures/rollback"

service_json="${fixtures}/service-canary.json"

assert_contains() {
  haystack="$1"
  needle="$2"
  case "$haystack" in
    *"$needle"*) ;;
    *) echo "expected output to contain: $needle" >&2; echo "$haystack" >&2; exit 1 ;;
  esac
}

run_resolve() {
  out=$(mktemp)
  dir=$(mktemp -d)
  status=0
  FIXTURE_MODE=1 \
    SERVICE_JSON_FILE="$1" \
    GITOPS_DIR="$2" \
    ROLLOUT_JSON_FILE="${3:-}" \
    STABLE_RS_JSON_FILE="${4:-}" \
    CANARY_RS_JSON_FILE="${5:-}" \
    STABLE_RECORD_JSON_FILE="${6:-}" \
    RELEASE_RECORD_BIN=false \
    ROLLBACK_OUTPUT_DIR="$dir" \
    "$script" resolve-target >/dev/null 2>"$out" || status=$?
  cat "$out"
  target="$dir/$(jq -r '.service' "$1").json"
  [ "$status" -eq 0 ] && [ -s "$target" ] && cat "$target"
  rm -f "$out"
  rm -rf "$dir"
  return "$status"
}

# L1: canary rollout + stable RS.
result=$(run_resolve "$service_json" "$repo_root" \
  "$fixtures/canary-rollout.json" "$fixtures/stable-rs.json" "$fixtures/canary-rs.json")
assert_contains "$result" '"source": "rollout-status"'
assert_contains "$result" '"image_digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111"'

# L1 blue-green: active selector cross-check passes.
result=$(run_resolve "$fixtures/service-bluegreen.json" "$repo_root" \
  "$fixtures/bluegreen-rollout.json" "$fixtures/bluegreen-rs.json")
assert_contains "$result" '"source": "rollout-status"'
assert_contains "$result" '"image_digest": "sha256:2222222222222222222222222222222222222222222222222222222222222222"'

# L2: no stableRS falls back to the PostgreSQL stable record.
result=$(run_resolve "$service_json" "$repo_root" \
  "$fixtures/no-stable-rollout.json" "" "" "$fixtures/stable-record.json")
assert_contains "$result" '"source": "postgres"'
assert_contains "$result" '"image_digest": "sha256:4444444444444444444444444444444444444444444444444444444444444444"'

# L3: no controller status and no record falls back to GitOps git history.
history=$(mktemp -d)
git -C "$history" init -q -b main
git -C "$history" -c user.name=test -c user.email=test@local config user.name test
git -C "$history" -c user.name=test -c user.email=test@local config user.email test@local
mkdir -p "$history/k3s/helm-values/workloads"
values_path="$history/k3s/helm-values/workloads/ecampus-topic.yaml"
cp "$fixtures/values-rollback.yaml" "$values_path"
git -C "$history" add .
git -C "$history" -c user.name=test -c user.email=test@local commit -qm base
git -C "$history" -c user.name=test -c user.email=test@local commit -q --allow-empty -m empty
sha=$(git -C "$history" rev-parse HEAD~1)
result=$(run_resolve "$service_json" "$history")
assert_contains "$result" '"source": "git-history"'
assert_contains "$result" "\"config_revision\": \"$sha\""
assert_contains "$result" '"image_digest": "sha256:3333333333333333333333333333333333333333333333333333333333333333"'

# No target anywhere -> exit 3.
empty=$(mktemp -d)
if run_resolve "$service_json" "$empty" "$fixtures/no-stable-rollout.json" >/dev/null 2>&1; then
  echo "expected resolve-target to fail without any rollback source" >&2
  exit 1
fi

# abort-traffic command selection.
canary_cmd=$(SERVICE_JSON_FILE="$service_json" ROLLOUTS_CLI=/rollouts KUBECTL_CLI=/kubectl DRY_RUN=1 "$script" abort-traffic)
assert_contains "$canary_cmd" '/rollouts abort ecampus-topic --namespace app'
undo_cmd=$(SERVICE_JSON_FILE="$service_json" ROLLOUTS_CLI=/rollouts KUBECTL_CLI=/kubectl UNDO_ROLLOUT=1 DRY_RUN=1 "$script" abort-traffic)
assert_contains "$undo_cmd" '/rollouts undo ecampus-topic --namespace app'
deploy_cmd=$(SERVICE_JSON_FILE="$fixtures/service-deployment.json" ROLLOUTS_CLI=/rollouts KUBECTL_CLI=/kubectl DRY_RUN=1 "$script" abort-traffic)
assert_contains "$deploy_cmd" '/kubectl rollout undo deployment/ecampus-theme --namespace app'

# verify-traffic: canary stable RS serves the digest and canary is scaled to 0.
if ! FIXTURE_MODE=1 \
  SERVICE_JSON_FILE="$service_json" \
  ROLLOUT_JSON_FILE="$fixtures/canary-rollout.json" \
  STABLE_RS_JSON_FILE="$fixtures/stable-rs.json" \
  CANARY_RS_JSON_FILE="$fixtures/canary-rs.json" \
  EXPECTED_DIGEST="sha256:1111111111111111111111111111111111111111111111111111111111111111" \
  "$script" verify-traffic; then
  echo "expected canary traffic verification to pass" >&2
  exit 1
fi

# verify-traffic: wrong digest fails.
if FIXTURE_MODE=1 \
  SERVICE_JSON_FILE="$service_json" \
  ROLLOUT_JSON_FILE="$fixtures/canary-rollout.json" \
  STABLE_RS_JSON_FILE="$fixtures/stable-rs.json" \
  CANARY_RS_JSON_FILE="$fixtures/canary-rs.json" \
  EXPECTED_DIGEST="sha256:9999999999999999999999999999999999999999999999999999999999999999" \
  "$script" verify-traffic >/dev/null 2>&1; then
  echo "expected digest mismatch to fail" >&2
  exit 1
fi

# verify-traffic: deployment.
if ! FIXTURE_MODE=1 \
  SERVICE_JSON_FILE="$fixtures/service-deployment.json" \
  DEPLOYMENT_JSON_FILE="$fixtures/deployment-rollback.json" \
  EXPECTED_DIGEST="sha256:5555555555555555555555555555555555555555555555555555555555555555" \
  "$script" verify-traffic; then
  echo "expected deployment traffic verification to pass" >&2
  exit 1
fi

# prepare-compensation: GitOps still points at the failed digest -> patch branch.
comp_repo=$(mktemp -d)
git -C "$comp_repo" init -q -b main
git -C "$comp_repo" -c user.name=test -c user.email=test@local config user.name test
git -C "$comp_repo" -c user.name=test -c user.email=test@local config user.email test@local
mkdir -p "$comp_repo/k3s/helm-values/workloads"
cp "$fixtures/values-failed.yaml" "$comp_repo/k3s/helm-values/workloads/ecampus-topic.yaml"
git -C "$comp_repo" add .
git -C "$comp_repo" -c user.name=test -c user.email=test@local commit -qm failed
target_file=$(mktemp)
cat > "$target_file" <<'EOF'
{"service":"topic","environment":"dev","image_digest":"sha256:3333333333333333333333333333333333333333333333333333333333333333","image_tag":"git-abcdef12","git_revision":"abcdef1234567890abcdef1234567890abcdef12","config_revision":"","rollout_strategy":"canary","source":"git-history"}
EOF
result=$(SERVICE_JSON_FILE="$service_json" GITOPS_DIR="$comp_repo" \
  COMPENSATION_BRANCH=rollback/topic/abcdef12 ROLLBACK_TARGET_FILE="$target_file" \
  COMPENSATION_BASE_REF=main "$script" prepare-compensation)
assert_contains "$result" "COMPENSATION_BRANCH=rollback/topic/abcdef12"
patched=$(yq '.image.digest // ""' "$comp_repo/k3s/helm-values/workloads/ecampus-topic.yaml")
[ "$patched" = "sha256:3333333333333333333333333333333333333333333333333333333333333333" ] || {
  echo "compensation values were not patched" >&2
  exit 1
}
if ! git -C "$comp_repo" diff --cached --quiet -- k3s/helm-values/workloads/ecampus-topic.yaml; then
  echo "compensation changed an unexpected file" >&2
  exit 1
fi
if [ "$(git -C "$comp_repo" diff main --name-only)" != "k3s/helm-values/workloads/ecampus-topic.yaml" ]; then
  echo "compensation diff touches more than the service values file" >&2
  exit 1
fi
git -C "$comp_repo" checkout -q -b rollback/topic/abcdef12 2>/dev/null || true

# prepare-compensation: GitOps already at the stable digest -> skip.
git -C "$comp_repo" branch -f main rollback/topic/abcdef12
result=$(SERVICE_JSON_FILE="$service_json" GITOPS_DIR="$comp_repo" \
  COMPENSATION_BRANCH=rollback/topic/abcdef12 ROLLBACK_TARGET_FILE="$target_file" \
  COMPENSATION_BASE_REF=main "$script" prepare-compensation)
assert_contains "$result" "COMPENSATION_SKIPPED=1"

echo "all rollback-release fixture tests passed"
