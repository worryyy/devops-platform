#!/bin/sh
# V1 ablation ladder driver: for each level, reset the right caches, trigger a
# build, wait for completion, record the duration. Results append to
# benchmarks/results/ci-timing.md. Levels:
#   L0 all-cold       - wipe buildkit local cache + go PVC caches, full matrix
#   L1 layer-cache    - wipe go caches only, full matrix
#   L2 all-warm       - no wipe, full matrix
#   L3 single-service - no wipe, theme-only SHA pair (impact analysis path)
# Two runs per level. SKIP_RELEASE=true everywhere (release direction deferred).
set -eu
JENKINS_URL=http://localhost:18080
JPASS=$(kubectl get secret jenkins -n delivery -o jsonpath='{.data.jenkins-admin-password}' | base64 -d)
BEFORE_SHA=94c062c50bb8e34d53953085806b8ff1151f053b
AFTER_SHA=0535b16be33679bbf05e1531005646338213a9ae
RESULTS="$(git rev-parse --show-toplevel)/benchmarks/results/ci-timing.md"

wipe_go() {
  ssh -o BatchMode=yes node3 'pvc=$(ls -d /var/lib/rancher/k3s/storage/pvc-*jenkins-agent-cache* | head -1); rm -rf "$pvc/go-build"/* "$pvc/go-mod"/*; echo wiped'
  echo "$(date +%H:%M) go caches wiped (host path)"
}

wipe_buildkit() {
  # wipe buildkitd state entirely: the daemon caches layers internally
  # (runc-overlayfs/cache.db), local-cache is only the portable export
  ssh -o BatchMode=yes node3 'pvc=$(ls -d /var/lib/rancher/k3s/storage/pvc-*buildkit-cache* | head -1); rm -rf "$pvc/local-cache"/* "$pvc/runc-overlayfs"/* "$pvc/cache.db" "$pvc/history.db"; echo wiped'
  echo "$(date +%H:%M) buildkit state wiped (host path)"
}

ensure_pf() {
  curl -s -o /dev/null --max-time 5 -u "admin:$JPASS" $JENKINS_URL/api/json && return 0
  pkill -f "port-forward svc/jenkins" 2>/dev/null
  nohup kubectl -n delivery port-forward svc/jenkins 18080:8080 >/dev/null 2>&1 &
  sleep 6
}

trigger_and_wait() {  # extra curl args via "$@"
  ensure_pf
  CJ=$(curl -s -c /tmp/jlad.jar -u "admin:$JPASS" $JENKINS_URL/crumbIssuer/api/json)
  CRUMB=$(echo "$CJ" | jq -r '.crumb'); FIELD=$(echo "$CJ" | jq -r '.crumbRequestField')
  curl -s -o /dev/null -b /tmp/jlad.jar -u "admin:$JPASS" -H "$FIELD: $CRUMB" -X POST \
    "$JENKINS_URL/job/ecampus-pipeline/buildWithParameters" "$@"
  # wait for a NEW build to appear and finish
  sleep 15
  N=$(curl -s -u "admin:$JPASS" "$JENKINS_URL/job/ecampus-pipeline/lastBuild/api/json" | jq -r .number)
  echo "$(date +%H:%M) build #$N started"
  while :; do
    sleep 60
    ensure_pf || continue
    J=$(curl -s --max-time 10 -u "admin:$JPASS" "$JENKINS_URL/job/ecampus-pipeline/$N/api/json" 2>/dev/null) || continue
    B=$(echo "$J" | jq -r '.building // empty' 2>/dev/null); R=$(echo "$J" | jq -r '.result // empty' 2>/dev/null)
    [ -z "$B" ] && continue
    [ "$B" = "false" ] && { DUR=$(echo "$J" | jq -r '.duration/1000' | cut -d. -f1); break; }
  done
  echo "$LEVEL run$RUN build#$N result=$R duration=${DUR}s"
  printf '| %s | %s | #%s | %s | %s |\n' "$(date '+%Y-%m-%d %H:%M')" "$LEVEL" "$N" "$R" "${DUR}s" >> "$RESULTS"
  [ "$R" = "SUCCESS" ] || { echo "LADDER ABORTED: build $N failed"; exit 1; }
}

run_level() {
  LEVEL=$1; RUN=$2; shift 2
  case "$LEVEL" in
    L0) wipe_buildkit "$LEVEL$RUN"; wipe_go "$LEVEL$RUN" ;;
    L1) wipe_go "$LEVEL$RUN" ;;
    L2|L3) : ;;
  esac
  if [ "$LEVEL" = "L3" ]; then
    trigger_and_wait --data-urlencode "SKIP_RELEASE=true" \
      --data-urlencode "BEFORE_SHA=$BEFORE_SHA" --data-urlencode "AFTER_SHA=$AFTER_SHA"
  else
    trigger_and_wait --data-urlencode "SKIP_RELEASE=true"
  fi
}

[ -f "$RESULTS" ] || printf '# CI ablation ladder (full matrix unless noted)\n\n| time | level | build | result | duration |\n|---|---|---|---|---|\n' > "$RESULTS"
START_AT=${START_AT:-L2}
for run in 1 2; do
  for lvl in L0 L1 L2 L3; do
    case "$START_AT:$lvl" in
      L1:L0|L2:L0|L2:L1|L3:L0|L3:L1|L3:L2) continue ;;
    esac
    run_level "$lvl" "$run"
  done
done
echo "LADDER COMPLETE"
