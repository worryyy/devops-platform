#!/usr/bin/env sh
# Validate the delivery-platform observability contract with pinned Prometheus
# and Alertmanager tool versions. Missing docker/yq is a hard failure; tests
# are never skipped.
set -eu

repo_root=$(git rev-parse --show-toplevel)
prom_values="${repo_root}/k3s/helm-values/platform/prometheus.yaml"
tests_dir="${repo_root}/k3s/ci/tests"
# The scratch dir must live under the repo (a colima-shared path): macOS
# mktemp dirs under /var/folders are not visible to the docker VM via
# virtiofs, and promtool would see an empty /work mount.
tmpdir=$(mktemp -d "${repo_root}/.tmp-observability.XXXXXX")
trap 'rm -rf "$tmpdir"' EXIT

# Pinned to the versions deployed by prometheus chart 29.14.0
# (server appVersion) and alertmanager sub-chart 1.40.3 (appVersion).
PROMTOOL_IMAGE=${PROMTOOL_IMAGE:-prom/prometheus:v3.13.0}
AMTOOL_IMAGE=${AMTOOL_IMAGE:-prom/alertmanager:v0.33.1}

command -v docker >/dev/null || { echo "docker is required to run observability checks" >&2; exit 1; }
command -v yq >/dev/null || { echo "yq is required to extract prometheus config" >&2; exit 1; }

yq -r '.serverFiles."recording_rules.yml"' "$prom_values" > "$tmpdir/recording_rules.yml"
yq -r '.serverFiles."alerting_rules.yml"' "$prom_values" > "$tmpdir/alerting_rules.yml"
yq -r 'del(.alertmanager.config.enabled) | .alertmanager.config' "$prom_values" > "$tmpdir/alertmanager.yml"
cp "${tests_dir}/alerting_rules_test.yml" "$tmpdir/"

echo "== promtool check rules =="
# --entrypoint is required: the prom/prometheus image entrypoint is the
# prometheus server binary, otherwise "promtool" becomes its argument.
docker run --rm -v "$tmpdir":/work -w /work --entrypoint promtool "$PROMTOOL_IMAGE" \
  check rules /work/recording_rules.yml /work/alerting_rules.yml

echo "== promtool test rules =="
docker run --rm -v "$tmpdir":/work -w /work --entrypoint promtool "$PROMTOOL_IMAGE" \
  test rules /work/alerting_rules_test.yml

echo "== amtool check-config =="
docker run --rm -v "$tmpdir":/work -w /work --entrypoint amtool "$AMTOOL_IMAGE" \
  check-config /work/alertmanager.yml

echo "== amtool config routes test =="
route_out=$(docker run --rm -v "$tmpdir":/work -w /work --entrypoint amtool "$AMTOOL_IMAGE" \
  config routes test --config.file=/work/alertmanager.yml \
  alertname=ReleasePodRestarting signal_type=deploy_noise deploy_id=ecampus-pipeline-main-1-comment-1 \
  service=comment namespace=app environment=dev)
printf '%s\n' "$route_out"
printf '%s' "$route_out" | grep -q 'default-receiver' || {
  echo "ReleasePodRestarting did not route to default-receiver" >&2
  exit 1
}
context_route_out=$(docker run --rm -v "$tmpdir":/work -w /work --entrypoint amtool "$AMTOOL_IMAGE" \
  config routes test --config.file=/work/alertmanager.yml \
  alertname=ReleaseDeployNoiseWindow signal_type=deploy_context deploy_id=ecampus-pipeline-main-1-comment-1 \
  service=comment namespace=app environment=dev)
printf '%s\n' "$context_route_out"
printf '%s' "$context_route_out" | grep -q 'deploy-context' || {
  echo "deploy_context alerts did not route to the config-less deploy-context receiver" >&2
  exit 1
}

echo "== inhibition contract =="
for rule_index in 0 1; do
  equal=$(yq -r ".inhibit_rules[$rule_index].equal | join(\",\")" "$tmpdir/alertmanager.yml")
  if [ "$equal" != "namespace,service,environment,deploy_id" ]; then
    echo "inhibit_rules[$rule_index].equal = $equal, want namespace,service,environment,deploy_id" >&2
    exit 1
  fi
done

source_matches=$(yq -r '[.inhibit_rules[].source_matchers[] | select(. == "deploy_id=~\".+\"")] | length' "$tmpdir/alertmanager.yml")
target_matches=$(yq -r '[.inhibit_rules[].target_matchers[] | select(. == "deploy_id=~\".+\"")] | length' "$tmpdir/alertmanager.yml")
[ "$source_matches" -ge 2 ] || { echo "every inhibit source must require a non-empty deploy_id" >&2; exit 1; }
[ "$target_matches" -ge 2 ] || { echo "every inhibit target must require a non-empty deploy_id" >&2; exit 1; }

scope_ok=$(yq -r '.inhibit_rules[1].source_matchers | any_c(. == "alert_scope=\"revision\"")' "$tmpdir/alertmanager.yml")
[ "$scope_ok" = "true" ] || {
  echo "only revision-scoped user_impact alerts may act as inhibit sources" >&2
  exit 1
}

grep -q 'alertname="ReleasePodRestarting"' "$tmpdir/alertmanager.yml" || {
  echo "inhibit targets must be limited to transient ReleasePodRestarting" >&2
  exit 1
}
if grep -Eq 'ReleasePodTerminating|ReleasePodCrashLooping|ReleasePodStuckTerminating|ReleaseReplicaShortage|ReleasePodNotReady' "$tmpdir/alertmanager.yml"; then
  echo "persistent or escalating deploy-noise alerts must never appear in inhibit targets" >&2
  exit 1
fi

noop_receiver=$(yq -r '.route.routes[] | select(.matchers | any_c(. == "signal_type=\"deploy_context\"")) | .receiver' "$tmpdir/alertmanager.yml")
[ -n "$noop_receiver" ] || {
  echo "deploy_context route must exist" >&2
  exit 1
}
receiver_field_count=$(yq -r ".receivers[] | select(.name == \"$noop_receiver\") | keys | length" "$tmpdir/alertmanager.yml")
[ "$receiver_field_count" = "1" ] || {
  echo "deploy_context receiver must have no notification configuration" >&2
  exit 1
}

echo "== alert durations contract =="
noise_for=$(yq -r '[.groups[].rules[] | select(.alert == "ReleaseDeployNoiseWindow") | has("for")] | any' "$tmpdir/alerting_rules.yml")
[ "$noise_for" = "false" ] || {
  echo "ReleaseDeployNoiseWindow is an internal context signal and must not have a for grace period" >&2
  exit 1
}
replica_for=$(yq -r '[.groups[].rules[] | select(.alert == "ReleaseReplicaShortage") | .for][0]' "$tmpdir/alerting_rules.yml")
[ "$replica_for" = "5m" ] || {
  echo "ReleaseReplicaShortage must keep a 5m grace period" >&2
  exit 1
}
notready_for=$(yq -r '[.groups[].rules[] | select(.alert == "ReleasePodNotReady") | .for][0]' "$tmpdir/alerting_rules.yml")
[ "$notready_for" = "5m" ] || {
  echo "ReleasePodNotReady must keep a 5m grace period" >&2
  exit 1
}
crash_for=$(yq -r '[.groups[].rules[] | select(.alert == "ReleasePodCrashLooping") | .for][0]' "$tmpdir/alerting_rules.yml")
[ "$crash_for" = "2m" ] || {
  echo "ReleasePodCrashLooping must keep a 2m grace period" >&2
  exit 1
}
stuck_for=$(yq -r '[.groups[].rules[] | select(.alert == "ReleasePodStuckTerminating") | .for][0]' "$tmpdir/alerting_rules.yml")
[ "$stuck_for" = "2m" ] || {
  echo "ReleasePodStuckTerminating must keep a 2m grace period" >&2
  exit 1
}

echo "== scrape/release metadata contract =="
grep -q 'kubelet_volume_stats_(used_bytes|capacity_bytes)' "$prom_values" || {
  echo "kubelet volume metrics scrape is missing" >&2
  exit 1
}
grep -q 'metricAnnotationsAllowlist' "$prom_values" || {
  echo "kube-state-metrics annotation allowlist is missing" >&2
  exit 1
}
grep -q 'delivery_platform_(deploy_id|git_sha|environment|release_batch|image_digest|gitops_revision)' "$prom_values" || {
  echo "generic pod labeldrop does not cover every release label" >&2
  exit 1
}
grep -q '__meta_kubernetes_pod_annotation_delivery_platform_image_digest' "$prom_values" || {
  echo "image digest relabel is missing from the application scrape jobs" >&2
  exit 1
}

echo "observability contract OK"
