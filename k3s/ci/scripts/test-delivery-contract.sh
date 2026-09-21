#!/usr/bin/env sh
set -eu

repo_root=$(git rev-parse --show-toplevel)
chart="${repo_root}/k3s/charts/go-service"
pipeline="${repo_root}/k3s/ci/jenkins/ecampus.Jenkinsfile"
release_rbac="${repo_root}/k3s/ci/jenkins/release-rbac.yaml"
buildkit_cache="${repo_root}/k3s/ci/jenkins/buildkit-cache.yaml"
wait_script="${repo_root}/k3s/ci/scripts/wait-for-release.sh"
rendered=$(mktemp)
trap 'rm -f "$rendered"' EXIT

count=0
for values in "${repo_root}"/k3s/helm-values/workloads/ecampus-*.yaml; do
  service=$(basename "$values" .yaml)
  test -f "${repo_root}/k3s/gitops/applications/workloads/${service}.yaml"
  helm lint "$chart" -f "$values" >/dev/null
  helm template "$service" "$chart" --namespace app -f "$values" >> "$rendered"
  count=$((count + 1))
done

test "$count" -eq 13
application_count=0
for application in "${repo_root}"/k3s/gitops/applications/workloads/ecampus-*.yaml; do
  test -f "$application"
  application_count=$((application_count + 1))
done
test "$application_count" -eq 13

# Every workload is a plain Deployment: no Rollout, AnalysisTemplate, canary
# ingress or stable/candidate service pair may come back.
test "$(grep -c '^kind: Deployment$' "$rendered")" -eq 13
test "$(grep -c '^kind: Rollout$' "$rendered")" -eq 0
test "$(grep -c '^kind: AnalysisTemplate$' "$rendered")" -eq 0
test "$(grep -c '^kind: Ingress$' "$rendered")" -eq 13
if grep -Eq 'canary|bluegreen|candidate|rollout-profile|canary-weight' "$rendered"; then
  echo "progressive delivery remnants remain in rendered workloads" >&2
  exit 1
fi

# Release identity contract: batch/deploy labels, downward API env, digest pinning.
grep -q 'delivery.platform/deploy-id' "$rendered"
grep -q 'delivery.platform/release-batch' "$rendered"
grep -q 'delivery.platform/git-sha' "$rendered"
grep -q 'delivery.platform/image-digest' "$rendered"
grep -q 'RELEASE_BATCH' "$rendered"

# Pipeline keeps the basic build → GitOps PR → wait-verify flow and nothing else.
grep -q 'moby/buildkit:v0.31.2-rootless' "$pipeline"
grep -q -- '--oci-worker-no-process-sandbox' "$pipeline"
grep -q -- '--opt platform=linux/amd64' "$pipeline"
grep -q -- '--metadata-file=' "$pipeline"
grep -q "imageMetadata\['containerimage.digest'\]" "$pipeline"
grep -q 'release-record' "$pipeline"
grep -q 'enablePullRequestAutoMerge' "$pipeline"
grep -q 'EXPECTED_DIGEST' "$pipeline"
grep -q 'DATABASE_URL' "$pipeline"
grep -q 'PROMETHEUS_URL' "$pipeline"
grep -q 'CONFIG_REV_' "$pipeline"
grep -q 'KUBECTL_CLI' "$pipeline"
grep -q 'gitopsRevisionAfterMerge' "$pipeline"
grep -q 'disableConcurrentBuilds' "$pipeline"
grep -q 'remote set-url origin' "$pipeline"
grep -q 'sha256sum "$kubectl_tmp"' "$pipeline"
grep -q 'wait-for-release.sh' "$pipeline"
grep -q 'sh "$WAIT_SCRIPT" wait' "$pipeline"
grep -q 'verify-metrics' "$wait_script"
grep -q 'evaluate-argocd' "$wait_script"
grep -q 'wait_for_argocd' "$wait_script"
grep -q 'EXPECTED_DIGEST' "$wait_script"
grep -q 'delivery.platform/git-sha' "$wait_script"
grep -q 'Argo CD did not apply' "$wait_script"
if grep -Eq 'STATE_FILE|main\.sha|PIPELINE_MODE|KANIKO_CACHE_REPO' "$pipeline"; then
  echo "legacy baseline or kaniko pipeline state remains" >&2
  exit 1
fi
if grep -Eq 'rollback-release|rollbackRelease|ROLLBACK|abortRelease|promote-wait|blue-green|bluegreen|manual_promotion|ROLLOUTS_CLI|rollout\.analysis|rolloutProfile|AnalysisRun|analysisrun|argo-rollouts|rollouts/|syncGuard|selfHeal' "$pipeline" "$wait_script"; then
  echo "progressive delivery or rollback code remains in the pipeline" >&2
  exit 1
fi
test ! -e "${repo_root}/k3s/ci/scripts/rollback-release.sh"
test ! -e "${repo_root}/k3s/ci/scripts/test-rollback-release.sh"
test ! -e "${repo_root}/k3s/ci/fixtures/rollback"
test ! -e "${repo_root}/k3s/gitops/applications/platform/argo-rollouts.yaml"
test ! -e "${repo_root}/k3s/helm-values/platform/argo-rollouts.yaml"
if grep -Eq -- 'release\.rolloutProfile|\.rollout\.' "$pipeline"; then
  echo "rollout values patching remains in the pipeline" >&2
  exit 1
fi
if grep -Eq '(^|[^[:alnum:]_])latest([^[:alnum:]_]|$)' "$pipeline"; then
  echo "pipeline must not publish or deploy latest" >&2
  exit 1
fi

# Release identity wiring between pipeline and values files.
grep -q 'RELEASE_BATCH=' "$pipeline"
grep -q "DEPLOY_ID=' + releaseBatch" "$pipeline"
grep -q '.release.releaseBatch = strenv(RELEASE_BATCH)' "$pipeline"
grep -q '.release.gitopsRevision = strenv(CONFIG_REVISION)' "$pipeline"

# The release ServiceAccount is read-only: no rollout subresources, no
# deployment writes, no Argo CD patching (rollback machinery is gone).
grep -q 'resources: \[deployments, deployments/status, replicasets\]' "$release_rbac"
grep -q 'resources: \[applications, applications/status\]' "$release_rbac"
grep -q 'namespace: argocd' "$release_rbac"
grep -q 'verbs: \[get, list, watch\]' "$release_rbac"
if grep -Eq 'rollouts|analysisruns|verbs: \[update|verbs: \[patch' "$release_rbac"; then
  echo "release RBAC still grants write or progressive-delivery permissions" >&2
  exit 1
fi

grep -q 'name: buildkitd-config' "$buildkit_cache"
grep -q 'name: buildkit-cache' "$buildkit_cache"
grep -q 'storage: 12Gi' "$buildkit_cache"
grep -q 'maxUsedSpace = "10GB"' "$buildkit_cache"
grep -q 'minFreeSpace = "3GB"' "$buildkit_cache"
grep -q 'claimName: buildkit-cache' "$pipeline"
grep -q 'secretName: tcr-kaniko-secret' "$pipeline"
grep -q 'fsGroup: 1000' "$pipeline"
grep -q 'mountPath: /home/user/.local/share/buildkit' "$pipeline"
grep -q 'mountPath: /home/user/.docker' "$pipeline"

# New delivery gates and release store.
test -f "${repo_root}/.github/workflows/ci.yml"
grep -q 'kubeconform' "${repo_root}/.github/workflows/ci.yml"
grep -q 'conftest' "${repo_root}/.github/workflows/ci.yml"
test -f "${repo_root}/platform/server/.golangci.yml"
test -f "${repo_root}/k3s/ci/policies/policy.rego"
test -f "${repo_root}/k3s/ci/policies/policy_test.rego"
test ! -e "${repo_root}/k3s/ci/schemas"
test -f "${repo_root}/platform/server/migrations/001_release_records.sql"
grep -q 'service_releases' "${repo_root}/platform/server/migrations/001_release_records.sql"
grep -q 'release_status' "${repo_root}/platform/server/migrations/001_release_records.sql"
if grep -q 'rollout_strategy' "${repo_root}/platform/server/migrations/001_release_records.sql"; then
  echo "release store still tracks rollout strategies" >&2
  exit 1
fi
test -f "${repo_root}/k3s/helm-values/platform/postgresql.yaml"
test -f "${repo_root}/k3s/secrets/platform-postgresql-auth.example.yaml"
grep -q 'release-record' "${repo_root}/platform/server/cmd/server/main.go"

# Catalog contract: services carry workloads and SLI budgets, no profiles.
catalog="${repo_root}/platform/server/configs/service-catalog.yaml"
grep -q 'workload: ecampus-' "$catalog"
if grep -Eq 'rolloutProfile|releaseProfiles|manualPromotion|previewProbes|canary|bluegreen' "$catalog"; then
  echo "progressive delivery fields remain in the service catalog" >&2
  exit 1
fi

# Observability contract: deploy-noise and user-impact alerts stay, release
# gate (analysis/rollout) alerts are gone.
prom_values="${repo_root}/k3s/helm-values/platform/prometheus.yaml"
test -f "${repo_root}/k3s/ci/scripts/test-observability.sh"
test -f "${repo_root}/k3s/ci/tests/alerting_rules_test.yml"
grep -q 'VersionHighErrorRate' "$prom_values"
grep -q 'ServiceHighErrorRate' "$prom_values"
grep -q 'VersionHighP95Latency' "$prom_values"
grep -q 'ReleaseReplicaShortage' "$prom_values"
grep -q 'ReleaseDeployNoiseWindow' "$prom_values"
if grep -Eq 'ReleaseAnalysis|ReleaseRolloutDegraded|rollout_info|analysis_run_phase' "$prom_values"; then
  echo "release-gate alert rules remain in the Prometheus config" >&2
  exit 1
fi
grep -q 'kubelet_volume_stats_(used_bytes|capacity_bytes)' "$prom_values"
grep -q 'metricAnnotationsAllowlist' "$prom_values"
grep -q 'delivery_platform_(deploy_id|git_sha|environment|release_batch|image_digest|gitops_revision)' "$prom_values"
grep -q '__meta_kubernetes_pod_annotation_delivery_platform_image_digest' "$prom_values"

echo "delivery contract OK"
