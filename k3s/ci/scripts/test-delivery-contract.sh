#!/usr/bin/env sh
set -eu

repo_root=$(git rev-parse --show-toplevel)
chart="${repo_root}/k3s/charts/go-service"
pipeline="${repo_root}/k3s/ci/jenkins/ecampus.Jenkinsfile"
release_rbac="${repo_root}/k3s/ci/jenkins/release-rbac.yaml"
buildkit_cache="${repo_root}/k3s/ci/jenkins/buildkit-cache.yaml"
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
test "$(grep -c '^kind: Rollout$' "$rendered")" -eq 12
test "$(grep -c '^kind: Deployment$' "$rendered")" -eq 1
test "$(grep -c '^kind: AnalysisTemplate$' "$rendered")" -eq 19
if grep -Eq 'strict-core|balanced-business|fast-noncore|rollout-prober|rollout\.profile\.' "$rendered"; then
  echo "legacy rollout analysis or profile fields remain in rendered workloads" >&2
  exit 1
fi

candidate_count=$(grep -c 'ecampus-.*-candidate' "$rendered")
test "$candidate_count" -ge 12
if grep -Eq '^  name: ecampus-[a-z]+-canary$' "$rendered"; then
  echo "legacy -canary candidate service naming remains in rendered workloads" >&2
  exit 1
fi

grep -q 'delivery.platform/rollout-profile: "critical-canary"' "$rendered"
grep -q 'delivery.platform/rollout-profile: "controlled-bluegreen"' "$rendered"
grep -q 'delivery.platform/rollout-profile: "fast-rolling"' "$rendered"
if grep -Eq 'risk-rules|initialDelay:|postPromotionDuration' "$rendered"; then
  echo "legacy risk-rule labels or dead analysis fields remain in rendered workloads" >&2
  exit 1
fi
grep -q 'name: stable-samples' "$rendered"
grep -q 'name: stable-error-rate' "$rendered"
grep -q 'name: stable-p95' "$rendered"
grep -q 'consecutiveErrorLimit' "$rendered"

# trivy image scanning was removed from the pipeline; no severity gate to assert
grep -q 'moby/buildkit:v0.31.2-rootless' "$pipeline"
grep -q -- '--oci-worker-no-process-sandbox' "$pipeline"
grep -q -- '--import-cache "type=registry,ref=\$CACHE_IMAGE"' "$pipeline"
grep -q -- '--export-cache "type=registry,ref=\$CACHE_IMAGE,mode=max,image-manifest=true,oci-mediatypes=true"' "$pipeline"
grep -q -- '--opt platform=linux/amd64' "$pipeline"
grep -q -- '--metadata-file=' "$pipeline"
grep -q "imageMetadata\['containerimage.digest'\]" "$pipeline"
grep -q 'BUILDKIT_CACHE_REPO' "$pipeline"
grep -q 'release-record' "$pipeline"
grep -q 'rollback-release.sh' "$pipeline"
grep -q 'enablePullRequestAutoMerge' "$pipeline"
grep -q 'COMPENSATION_SKIPPED' "$pipeline"
grep -q 'EXPECTED_DIGEST' "$pipeline"
grep -q 'DATABASE_URL' "$pipeline"
grep -q 'PROMETHEUS_URL' "$pipeline"
grep -q 'CONFIG_REV_' "$pipeline"
if grep -Eq 'STATE_FILE|main\.sha|Advance successful baseline|PIPELINE_MODE|KANIKO_CACHE_REPO' "$pipeline"; then
  echo "legacy baseline or kaniko pipeline state remains" >&2
  exit 1
fi
grep -q 'claimName: buildkit-cache' "$pipeline"
grep -q 'secretName: tcr-kaniko-secret' "$pipeline"
grep -q 'fsGroup: 1000' "$pipeline"
grep -q 'runAsUser: 1000' "$pipeline"
grep -q 'seccompProfile:' "$pipeline"
grep -q 'appArmorProfile:' "$pipeline"
grep -q 'mountPath: /home/user/.local/share/buildkit' "$pipeline"
grep -q 'mountPath: /home/user/.docker' "$pipeline"
if grep -Eq '/kaniko/executor|KANIKO_CACHE_REPO|name: kaniko|--build=' "$pipeline"; then
  echo "legacy image build configuration remains in the BuildKit pipeline" >&2
  exit 1
fi
grep -q 'name: buildkitd-config' "$buildkit_cache"
grep -q 'name: buildkit-cache' "$buildkit_cache"
grep -q 'storage: 30Gi' "$buildkit_cache"
grep -q 'maxUsedSpace = "25GB"' "$buildkit_cache"
grep -q 'minFreeSpace = "5GB"' "$buildkit_cache"
grep -q '"$ROLLOUTS_CLI" abort' "$pipeline"
grep -q 'wait-for-release.sh' "$pipeline"
grep -q 'sh "$WAIT_SCRIPT" wait' "$pipeline"
grep -q 'sh "$WAIT_SCRIPT" promote-wait' "$pipeline"
grep -q 'verify-metrics' "${repo_root}/k3s/ci/scripts/wait-for-release.sh"
grep -q 'evaluate-argocd' "${repo_root}/k3s/ci/scripts/wait-for-release.sh"
grep -q 'wait_for_argocd' "${repo_root}/k3s/ci/scripts/wait-for-release.sh"
grep -q 'EXPECTED_DIGEST' "${repo_root}/k3s/ci/scripts/wait-for-release.sh"
grep -q 'resolve-target' "${repo_root}/k3s/ci/scripts/rollback-release.sh"
grep -q 'abort-traffic' "${repo_root}/k3s/ci/scripts/rollback-release.sh"
grep -q 'verify-traffic' "${repo_root}/k3s/ci/scripts/rollback-release.sh"
grep -q 'prepare-compensation' "${repo_root}/k3s/ci/scripts/rollback-release.sh"
test -x "${repo_root}/k3s/ci/scripts/rollback-release.sh"
test -x "${repo_root}/k3s/ci/scripts/test-rollback-release.sh"
grep -q 'KUBECTL_CLI' "$pipeline"
if grep -Eq -- 'release\.riskRules|RISK_RULES|matched_risk_rules|required_checks' "$pipeline"; then
  echo "legacy impact/risk-rule wiring remains in the pipeline" >&2
  exit 1
fi
grep -q 'manual_promotion' "$pipeline"
grep -q 'blue-green promotion' "$pipeline"
grep -q 'rollout.analysis.dryRun' "$pipeline"
grep -q 'consecutiveErrorLimit' "$pipeline"
grep -q 'delivery.platform/git-sha' "${repo_root}/k3s/ci/scripts/wait-for-release.sh"
grep -q 'Argo CD did not apply' "${repo_root}/k3s/ci/scripts/wait-for-release.sh"
grep -q 'remote set-url origin' "$pipeline"
grep -q 'sha256sum -c' "$pipeline"
grep -q 'gitopsRevisionAfterMerge' "$pipeline"
grep -q 'disableConcurrentBuilds' "$pipeline"
if grep -Eq 'new rollout was not observed|waitForService' "$pipeline"; then
  echo "legacy rollout orchestration remains in the pipeline" >&2
  exit 1
fi
grep -q 'resources: \[deployments, deployments/status, replicasets\]' "$release_rbac"
grep -q 'resources: \[analysisruns, analysisruns/status\]' "$release_rbac"
grep -q 'resources: \[rollouts/promote, rollouts/abort, rollouts/restart\]' "$release_rbac"
grep -q 'resources: \[rollouts/undo\]' "$release_rbac"
grep -q 'verbs: \[update\]' "$release_rbac"
grep -q 'resources: \[applications, applications/status\]' "$release_rbac"
grep -q 'namespace: argocd' "$release_rbac"
grep -q 'verbs: \[get, list, watch\]' "$release_rbac"
if grep -Eq '(^|[^[:alnum:]_])latest([^[:alnum:]_]|$)' "$pipeline"; then
  echo "pipeline must not publish or deploy latest" >&2
  exit 1
fi

# New delivery gates and release store.
test -f "${repo_root}/.github/workflows/ci.yml"
grep -q 'deploy-gate' "${repo_root}/.github/workflows/ci.yml"
grep -q 'kubeconform' "${repo_root}/.github/workflows/ci.yml"
grep -q 'conftest' "${repo_root}/.github/workflows/ci.yml"
grep -q 'helm-render' "${repo_root}/.github/workflows/ci.yml"
test -f "${repo_root}/platform/server/.golangci.yml"
test -f "${repo_root}/k3s/ci/policies/policy.rego"
test -f "${repo_root}/k3s/ci/policies/policy_test.rego"
test -f "${repo_root}/k3s/ci/schemas/rollout_v1alpha1.json"
test -f "${repo_root}/k3s/ci/schemas/analysistemplate_v1alpha1.json"
test -f "${repo_root}/platform/server/migrations/001_release_records.sql"
grep -q 'service_releases' "${repo_root}/platform/server/migrations/001_release_records.sql"
grep -q 'release_status' "${repo_root}/platform/server/migrations/001_release_records.sql"
test -f "${repo_root}/k3s/helm-values/platform/postgresql.yaml"
test -f "${repo_root}/k3s/secrets/platform-postgresql-auth.example.yaml"
grep -q 'release-record' "${repo_root}/platform/server/cmd/server/main.go"

# The go-service chart must render manifests that match the Argo Rollouts
# CRD: analysis is its own canary step, not a pause field, and pre-promotion
# analysis carries no timeoutSeconds.
if grep -Eq 'pause:.*analysis|timeoutSeconds' "${repo_root}/k3s/charts/go-service/templates/_helpers.tpl"; then
  echo "chart still renders Rollout fields rejected by the Argo Rollouts CRD" >&2
  exit 1
fi
grep -q -- '- analysis:' "${repo_root}/k3s/charts/go-service/templates/_helpers.tpl"

# Release identity contract: batch label/env, pipeline batch/deploy ids, and
# the observability pipeline (Prometheus relabels, Loki/Alloy, unit tests).
grep -q 'delivery.platform/release-batch' "$rendered"
grep -q 'RELEASE_BATCH' "$rendered"
grep -q 'RELEASE_BATCH=' "$pipeline"
grep -q "DEPLOY_ID=' + releaseBatch" "$pipeline"
grep -q '.release.releaseBatch = strenv(RELEASE_BATCH)' "$pipeline"
grep -q '.release.gitopsRevision = strenv(CONFIG_REVISION)' "$pipeline"

prom_values="${repo_root}/k3s/helm-values/platform/prometheus.yaml"
loki_values="${repo_root}/k3s/helm-values/platform/loki.yaml"
alloy_values="${repo_root}/k3s/helm-values/platform/alloy.yaml"
test -f "$prom_values"
test -f "$loki_values"
test -f "$alloy_values"
test -f "${repo_root}/k3s/ci/scripts/test-observability.sh"
test -f "${repo_root}/k3s/ci/tests/alerting_rules_test.yml"

grep -q 'VersionHighErrorRate' "$prom_values"
grep -q 'ServiceHighErrorRate' "$prom_values"
grep -q 'VersionHighP95Latency' "$prom_values"
grep -q 'ReleaseReplicaShortage' "$prom_values"
grep -q 'ReleaseAnalysisFailed' "$prom_values"
grep -q 'ReleaseAnalysisInconclusiveOrError' "$prom_values"
grep -q 'rollout_info{phase="Degraded"}' "$prom_values"
grep -q 'kubelet_volume_stats_(used_bytes|capacity_bytes)' "$prom_values"
grep -q 'metricAnnotationsAllowlist' "$prom_values"
grep -q 'delivery_platform_(deploy_id|git_sha|environment|release_batch|image_digest|gitops_revision)' "$prom_values"
grep -q '__meta_kubernetes_pod_annotation_delivery_platform_image_digest' "$prom_values"
grep -q -- '- release_batch' "$prom_values"

grep -q 'deploymentMode: Monolithic' "$loki_values"
grep -q 'retention_period: 168h' "$loki_values"
grep -q 'networkPolicy:' "$loki_values"
grep -q '100.67.223.96/32' "$loki_values"

grep -q 'type: deployment' "$alloy_values"
grep -q 'loki.source.kubernetes' "$alloy_values"
grep -q 'stage.structured_metadata' "$alloy_values"
grep -q 'deploy_id' "$alloy_values"
