#!/usr/bin/env sh
# Measure the delivery pipeline timing claims with real cluster runs.
#
# Three ablation runs produce the resume numbers:
#   cold-full   - fresh BuildKit cache tag + empty BEFORE/AFTER SHA, which
#                 forces the conservative full build (the pre-optimization
#                 baseline: every service rebuilt, no layer cache)
#   warm-full   - same cache tag as the cold run, repeated: isolates the
#                 cache contribution (all services, layers reused)
#   warm-impact - production path: main-amd64 cache + BEFORE/AFTER SHA of a
#                 small commit, so only the affected services rebuild
#
# Every run reports: Jenkins queue time, build duration, and the end-to-end
# "build start -> first new-version pod Ready" latency derived from the
# deploy_id pod labels (only the new release carries that label).
#
# Required environment:
#   JENKINS_URL   e.g. http://localhost:8080 (kubectl port-forward first)
#   JENKINS_AUTH  user:api-token
# Optional environment:
#   JENKINS_JOB   defaults to ecampus-pipeline
#   NAMESPACE     defaults to app
#   SOURCE_REPO   override the source repository (e.g. the benchmark mirror)
# Results are appended to benchmarks/results/ci-timing.md under the repo root.
set -eu

JENKINS_URL=${JENKINS_URL:?JENKINS_URL must be set (port-forward to Jenkins first)}
JENKINS_AUTH=${JENKINS_AUTH:?JENKINS_AUTH must be set as user:api-token}
JENKINS_JOB=${JENKINS_JOB:-ecampus-pipeline}
NAMESPACE=${NAMESPACE:-app}
BEFORE_SHA=${BEFORE_SHA:-}
AFTER_SHA=${AFTER_SHA:-}
SOURCE_REPO=${SOURCE_REPO:-}

repo_root=$(git rev-parse --show-toplevel)
results_dir="${repo_root}/benchmarks/results"
mkdir -p "$results_dir"
results_file="${results_dir}/ci-timing.md"
[ -f "$results_file" ] || printf '# CI pipeline timing benchmark\n\n| run | mode | build | queue(s) | build(s) | first-new-pod-ready(s) | e2e(s) | services |\n|---|---|---|---|---|---|---|---|\n' > "$results_file"

usage() {
  cat >&2 <<EOF
usage: $0 <mode>
  cold-full   [CACHE_TAG=fresh] full build with a cold registry cache (baseline)
  warm-full   CACHE_TAG=<tag of a previous cold-full run> full build, warm cache
  warm-impact production path with impact detection (set BEFORE_SHA/AFTER_SHA)
  reset-go-caches  wipe the Go build/module caches on the jenkins-agent-cache PVC
EOF
  exit 1
}

jenkins_json() {
  curl -fsS -u "$JENKINS_AUTH" "$JENKINS_URL/$1"
}

trigger_build() {
  mode=$1
  cache_tag=$2
  case "$mode" in
    warm-impact)
      [ -n "$BEFORE_SHA" ] && [ -n "$AFTER_SHA" ] || {
        echo "warm-impact needs BEFORE_SHA and AFTER_SHA of a small commit" >&2
        exit 1
      }
      ;;
    *)
      BEFORE_SHA=''
      AFTER_SHA=''
      ;;
  esac
  # shellcheck disable=SC2086
  queue_url=$(curl -fsS -u "$JENKINS_AUTH" -X POST \
    "${JENKINS_URL}/job/${JENKINS_JOB}/buildWithParameters" \
    --data-urlencode "BUILDKIT_CACHE_TAG=${cache_tag}" \
    ${SOURCE_REPO:+--data-urlencode "SOURCE_REPO=${SOURCE_REPO}"} \
    ${BEFORE_SHA:+--data-urlencode "BEFORE_SHA=${BEFORE_SHA}"} \
    ${AFTER_SHA:+--data-urlencode "AFTER_SHA=${AFTER_SHA}"} \
    -o /dev/null -w '%{redirect_url}')
  queue_item=${queue_url#${JENKINS_URL}/queue/item/}
  queue_item=${queue_item%/}
  echo "$queue_item"
}

# Wait for the queued item to become a concrete build number.
build_number_from_queue() {
  queue_item=$1
  while :; do
    build=$(curl -fsS -u "$JENKINS_AUTH" \
      "${JENKINS_URL}/queue/item/${queue_item}/api/json" |
      jq -r '.executable.number // empty')
    [ -n "$build" ] && { echo "$build"; return; }
    sleep 5
  done
}

wait_for_build() {
  build=$1
  while :; do
    result=$(jenkins_json "/job/${JENKINS_JOB}/${build}/api/json" | jq -r '.result // empty')
    [ "$result" != "null" ] && [ -n "$result" ] && { echo "$result"; return; }
    sleep 15
  done
}

# first-ready timestamp (epoch seconds) among pods of this deploy_id
first_pod_ready_epoch() {
  deploy_id=$1
  kubectl get pods -n "$NAMESPACE" -l "delivery.platform/deploy-id=${deploy_id}" -o json |
    jq -r '[.items[].status.conditions[]
       | select(.type == "Ready" and .status == "True")
       | .lastTransitionTime // empty] | min // empty' |
    sed 's/\.[0-9]*Z$/Z/' |
    while IFS= read -r ts; do
      [ -n "$ts" ] && date -j -f '%Y-%m-%dT%H:%M:%SZ' "$ts" +%s 2>/dev/null || true
    done | sort -n | head -1
}

affected_services() {
  build=$1
  jenkins_json "/job/${JENKINS_JOB}/${build}/consoleText" |
    sed -n 's/.*build services: \(.*\)$/\1/p' | tail -1
}

main() {
  [ $# -eq 1 ] || usage
  mode=$1

  if [ "$mode" = "reset-go-caches" ]; then
    echo "This wipes /cache/go-build and /cache/go-mod on jenkins-agent-cache." >&2
    printf 'Type RESET to continue: ' >&2
    read -r answer
    [ "$answer" = "RESET" ] || { echo "aborted" >&2; exit 1; }
    kubectl run reset-go-caches --rm -i --restart=Never \
      --overrides='{"spec":{"volumes":[{"name":"c","persistentVolumeClaim":{"claimName":"jenkins-agent-cache"}}],"containers":[{"name":"r","image":"alpine:3.21","command":["sh","-c","rm -rf /cache/go-build/* /cache/go-mod/* && echo caches wiped"],"volumeMounts":[{"name":"c","mountPath":"/cache"}]}]}}' \
      --image=alpine:3.21
    exit 0
  fi

  case "$mode" in
    cold-full) cache_tag="bench-cold-$(date +%s)" ;;
    warm-full)
      cache_tag=${CACHE_TAG:?warm-full needs CACHE_TAG of the earlier cold-full run}
      ;;
    warm-impact) cache_tag=${CACHE_TAG:-main-amd64} ;;
    *) usage ;;
  esac

  echo "== triggering ${mode} (cache tag: ${cache_tag}) =="
  queue_item=$(trigger_build "$mode" "$cache_tag")
  queue_started=$(date +%s)
  build=$(build_number_from_queue "$queue_item")

  build_json=$(jenkins_json "/job/${JENKINS_JOB}/${build}/api/json")
  build_start_ms=$(printf '%s' "$build_json" | jq -r '.timestamp')
  queue_s=$((build_start_ms / 1000 - queue_started))

  echo "== build #${build} running (queue wait ${queue_s}s) =="
  result=$(wait_for_build "$build")
  build_json=$(jenkins_json "/job/${JENKINS_JOB}/${build}/api/json")
  build_s=$(printf '%s' "$build_json" | jq -r '.duration' | awk '{printf "%d", $1 / 1000}')

  services=$(affected_services "$build")
  [ -n "$services" ] || services='?'
  service_count=$(printf '%s\n' "$services" | awk -F',' 'NF > 0 {print NF}')

  first_ready_s=''
  if [ "$result" = "SUCCESS" ] && [ -n "$service_count" ] && [ "$service_count" -ge 1 ] && [ "$services" != "?" ]; then
    first_service=$(printf '%s' "$services" | cut -d',' -f1)
    deploy_id="${JENKINS_JOB}-${build}-${first_service}-1"
    # wait up to 10 minutes for the first new-version pod to become Ready
    tries=40
    while [ "$tries" -gt 0 ]; do
      ready=$(first_pod_ready_epoch "$deploy_id")
      if [ -n "$ready" ]; then
        first_ready_s=$((ready - build_start_ms / 1000))
        break
      fi
      tries=$((tries - 1))
      sleep 15
    done
  fi

  e2e_s=''
  [ -n "$first_ready_s" ] && e2e_s=$((queue_s + first_ready_s))

  printf '| %s | %s | #%s | %s | %s | %s | %s | %s |\n' \
    "$(date '+%Y-%m-%d %H:%M')" "$mode" "$build" \
    "${queue_s}" "${build_s}" "${first_ready_s:-n/a}" "${e2e_s:-n/a}" "$services" |
    tee -a "$results_file"

  echo
  echo "result: ${result}  (details appended to ${results_file})"
  [ "$result" = "SUCCESS" ] || exit 1
}

main "$@"
