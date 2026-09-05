#!/usr/bin/env sh
# Measure the release-window alert governance claims on the live cluster.
#
# observe <minutes>       Poll Prometheus and Alertmanager for the window and
#                         report, per alertname: unique alerts FIRED (Prometheus)
#                         vs INHIBITED (Alertmanager inhibitedBy non-empty) vs
#                         NOTIFIED (= fired - inhibited). Run it while a release
#                         is in progress or right after injecting faults.
# noise-window <service>  rollout-restart a service so fresh pods open the
#                         ReleaseDeployNoiseWindow (context signal, never notified)
# inject-restart <svc>    kill PID 1 once in one pod -> transient restart, the
#                         alert class that inhibition is allowed to suppress
# inject-crashloop <svc>  kill PID 1 four times -> CrashLoop pattern, the alert
#                         class that must NEVER be suppressed
#
# The resume metric is produced by observe:
#   - deploy_context signals fired vs notified (routing mechanism)
#   - deploy_noise fired vs notified (inhibition mechanism)
#   - persistent classes (CrashLooping/StuckTerminating/NotReady/ReplicaShortage)
#     and user_impact: inhibited must stay 0 (safety assertion)
#
# Required environment:
#   PROM_URL  e.g. http://localhost:9090 (kubectl port-forward first)
#   AM_URL    e.g. http://localhost:9093
# Optional environment:
#   NAMESPACE defaults to app
# Results are appended to benchmarks/results/alert-noise.md under the repo root.
set -eu

PROM_URL=${PROM_URL:?PROM_URL must be set (port-forward to Prometheus first)}
AM_URL=${AM_URL:?AM_URL must be set (port-forward to Alertmanager first)}
NAMESPACE=${NAMESPACE:-app}

repo_root=$(git rev-parse --show-toplevel)
results_dir="${repo_root}/benchmarks/results"
mkdir -p "$results_dir"
results_file="${results_dir}/alert-noise.md"

usage() {
  sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//' >&2
  exit 1
}

prom_get() { curl -fsS "$PROM_URL/api/v1/$1"; }
am_get() { curl -fsS "$AM_URL/api/v2/$1"; }

# unique key: alertname|namespace|service|pod|container|deploy_id
prom_keys() {
  prom_get alerts |
    jq -r '.data.alerts[]
      | select(.state == "firing")
      | [.labels.alertname,
         .labels.namespace // "-",
         .labels.service // "-",
         .labels.pod // "-",
         .labels.container // "-",
         .labels.deploy_id // "-"]
      | join("|")'
}

am_keys() {
  am_get alerts |
    jq -r '.[]
      | select(.status.state == "active")
      | [(.labels.alertname // "-"),
         (.labels.namespace // "-"),
         (.labels.service // "-"),
         (.labels.pod // "-"),
         (.labels.container // "-"),
         (.labels.deploy_id // "-"),
         (if (.status.inhibitedBy // []) | length > 0 then "INHIBITED" else "CLEAN" end)]
      | join("|")'
}

pick_pod() {
  # Newest running pod: the noise window only covers pods younger than 900s,
  # so fault injection must target a freshly created pod to stay inside it.
  kubectl get pods -n "$NAMESPACE" -l "app.kubernetes.io/name=$1" \
    -o json | jq -r '[.items[]
      | select(.status.phase == "Running")]
      | sort_by(.metadata.creationTimestamp) | .[-1].metadata.name // empty'
}

kill_pid1() {
  pod=$1
  echo "kill 1 in ${NAMESPACE}/${pod}"
  kubectl exec -n "$NAMESPACE" "$pod" -- /bin/sh -c 'kill 1' 2>/dev/null ||
    kubectl exec -n "$NAMESPACE" "$pod" -c "$(kubectl get pod -n "$NAMESPACE" "$pod" -o jsonpath='{.spec.containers[0].name}')" -- /bin/sh -c 'kill 1'
}

observe() {
  minutes=$1
  work=$(mktemp -d)
  trap 'rm -rf "$work"' EXIT
  : >"$work/prom.txt"
  : >"$work/am.txt"

  ends=$(( $(date +%s) + minutes * 60 ))
  poll=0
  while [ "$(date +%s)" -lt "$ends" ]; do
    prom_keys >>"$work/prom.txt" || true
    am_keys >>"$work/am.txt" || true
    poll=$((poll + 1))
    sleep 15
  done

  echo "== observed ${poll} polls over ${minutes}m =="
  cat "$work/prom.txt" | sort -u >"$work/prom.u"
  cat "$work/am.txt" | sort -u >"$work/am.u"

  # signal_type lookup per alertname, fetched once from the rule definitions
  prom_get rules |
    jq -r '.data.groups[].rules[] | select(.name) | "\(.name)|\(.labels.signal_type // "-")"' |
    sort -u >"$work/signals.txt"

  # per alertname fired / inhibited / notified
  report="$(
    cut -d'|' -f1 "$work/prom.u" | sort | uniq -c | sort -rn |
      while read -r fired alert; do
        inhibited=$(grep "^${alert}|" "$work/am.u" | grep -c '|INHIBITED$' || true)
        signal=$(awk -F'|' -v a="$alert" '$1 == a {print $2; exit}' "$work/signals.txt")
        notified=$((fired - inhibited))
        printf '%s|%s|%s|%s|%s|%s\n' "$alert" "${signal:-?}" "$fired" "$inhibited" "$notified" \
          "$((fired > 0 ? inhibited * 100 / fired : 0))"
      done
  )"

  printf '\n## %s\n\n| alertname | signal_type | fired | inhibited | notified | inhibit-rate %% |\n|---|---|---|---|---|---|\n' \
    "$(date '+%Y-%m-%d %H:%M')" >>"$results_file"
  printf '%s\n' "$report" | tee -a "$results_file"

  echo
  echo "== safety assertions =="
  fail=0
  for alert in ReleasePodCrashLooping ReleasePodStuckTerminating ReleasePodNotReady ReleaseReplicaShortage VersionHighErrorRate VersionHighP95Latency ServiceHighErrorRate; do
    n=$(printf '%s\n' "$report" | awk -F'|' -v a="$alert" '$1 == a {print $4}')
    [ "${n:-0}" -gt 0 ] 2>/dev/null && {
      echo "FAIL: ${alert} was inhibited ${n} times - persistent faults must never be suppressed"
      fail=1
    }
  done
  [ "$fail" -eq 0 ] && echo "PASS: no persistent or user-impact alert was inhibited"
  echo "results appended to ${results_file}"
  exit "$fail"
}

main() {
  [ $# -ge 1 ] || usage
  cmd=$1
  case "$cmd" in
    observe)
      [ $# -eq 2 ] || usage
      observe "$2"
      ;;
    noise-window)
      [ $# -eq 2 ] || usage
      echo "restarting $2 to open a fresh release noise window"
      kubectl rollout restart -n "$NAMESPACE" "deployment/$2" 2>/dev/null ||
        kubectl argo rollouts restart "$2" -n "$NAMESPACE"
      ;;
    inject-restart)
      [ $# -eq 2 ] || usage
      pod=$(pick_pod "$2")
      [ -n "$pod" ] || { echo "no running pod for $2" >&2; exit 1; }
      kill_pid1 "$pod"
      echo "transient restart injected; ReleasePodRestarting should fire (~2m for) and be inhibited while the noise window is open"
      ;;
    inject-crashloop)
      [ $# -eq 2 ] || usage
      pod=$(pick_pod "$2")
      [ -n "$pod" ] || { echo "no running pod for $2" >&2; exit 1; }
      i=0
      while [ "$i" -lt 4 ]; do
        pod=$(pick_pod "$2")
        echo "--- crash iteration $((i + 1)) ---"
        kill_pid1 "$pod"
        i=$((i + 1))
        [ "$i" -lt 4 ] && sleep 90
      done
      echo "crash-loop pattern injected (4 restarts); ReleasePodCrashLooping must fire and never be inhibited"
      ;;
    *)
      usage
      ;;
  esac
}

main "$@"
