#!/usr/bin/env sh
# wait-for-release.sh — wait for a single service release without growing the
# Jenkins Groovy. Parses Rollout, AnalysisRun and Deployment JSON directly.
#
# Subcommands:
#   wait            poll the cluster until the release is healthy or aborted
#   evaluate        pure verdict function over local JSON files (fixture tests)
#   approval-wait   poll an approval marker file until it appears or times out
#   evaluate-digest pure digest assertion over local workload JSON
#   evaluate-argocd pure Argo CD sync/health assertion over local JSON
#   verify-metrics  poll Prometheus until the released service meets SLI
#
# Verdicts:
#   healthy | failed | error | inconclusive | pending | timeout | approval-timeout
#
# Inputs (environment):
#   SERVICE_JSON_FILE      one entry of delivery-catalog.json
#   EXPECTED_GIT_SHA       git sha the GitOps pipeline published
#   EXPECTED_DIGEST        image digest the GitOps pipeline published
#   ARGOCD_APP             Argo CD Application name
#   ARGOCD_NAMESPACE       Argo CD namespace (default argocd)
#   CONFIG_REVISION        GitOps revision Argo CD must sync
#   PROMETHEUS_URL         Prometheus base URL for post-release metrics
#   METRICS_OBSERVATION_SECONDS  default 300
#   METRICS_POLL_SECONDS         default 30
#   ROLLOUTS_CLI           kubectl-argo-rollouts binary
#   KUBECTL_CLI            kubectl binary
#   APPROVAL_MARKER        file created when the blue-green approval happens
#   APPROVAL_TIMEOUT       seconds to wait for the approval marker
#   FIXTURE_MODE=1         read JSON from ROLLOUT_JSON_FILE/ANALYSISRUN_JSON_FILE
#   ROLLOUT_JSON_FILE      fixture Rollout JSON
#   ANALYSISRUN_JSON_FILE  fixture AnalysisRun JSON
#   DEPLOYMENT_JSON_FILE   fixture Deployment JSON
#   ARGOCD_APPLICATION_JSON_FILE fixture Application JSON
#   METRICS_JSON_FILE      fixture {"samples":..,"error_rate":..,"p95":..}

set -eu

WAIT_SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

service_json() {
  jq -r "$1" "$SERVICE_JSON_FILE"
}

workload_kind() { service_json '.workload_kind'; }
rollout_name() { service_json '.rollout'; }
resource_name() { service_json '.resource_name'; }
namespace() { service_json '.namespace'; }
wait_timeout() { service_json '.wait_timeout'; }
stable_service() { service_json '.stable_service'; }
health_path() { service_json '.health_path'; }

# duration like "75m" to seconds; defaults to 900.
seconds_from_duration() {
  case "$1" in
    *m) printf '%s' "$(( ${1%m} * 60 ))" ;;
    *h) printf '%s' "$(( ${1%h} * 3600 ))" ;;
    *s) printf '%s' "$((${1%s}))" ;;
    "") printf '900' ;;
    *) printf '%s' "$1" ;;
  esac
}

abort_release() {
  "$ROLLOUTS_CLI" abort "$(rollout_name)" --namespace "$(namespace)" || true
  echo "aborted $(rollout_name)"
}

evaluate_rollout() {
  # $1: rollout json file, $2: analysisrun json file, $3: deployment json file,
  # $4: analysisrun role (step|pre|post|auto, default auto)
  rollout_json="$1"
  analysisrun_json="$2"
  deployment_json="$3"
  ar_role="${4:-auto}"
  kind=$(jq -r '.kind' "$rollout_json")

  if [ "$kind" = "Deployment" ]; then
    if [ -f "$deployment_json" ] && [ "$(jq -r '.status.conditions[]? | select(.type == "Progressing") | .reason' "$deployment_json")" = "ProgressDeadlineExceeded" ]; then
      echo "failed"
      return
    fi
    if [ -f "$deployment_json" ] && [ "$(jq -r '.status.availableReplicas // 0' "$deployment_json")" -ge "$(jq -r '.status.replicas // 0' "$deployment_json")" ] && [ "$(jq -r '.status.replicas // 0' "$deployment_json")" -gt 0 ]; then
      echo "healthy"
      return
    fi
    echo "pending"
    return
  fi

  phase=$(jq -r '.status.phase // ""' "$rollout_json")
  if [ "$phase" = "Degraded" ]; then
    echo "failed"
    return
  fi

  if [ -f "$analysisrun_json" ] && [ "$(jq -r '.kind // ""' "$analysisrun_json")" = "AnalysisRun" ]; then
    ar_phase=$(jq -r '.status.phase // ""' "$analysisrun_json")
    case "$ar_phase" in
      Failed) echo "failed"; return ;;
      Error) echo "error"; return ;;
      Inconclusive) echo "inconclusive"; return ;;
      Successful)
        # A successful step analysis only matters once the whole rollout is
        # Healthy; a successful blue-green pre-promotion analysis means the
        # preview passed and the release waits for human approval.
        case "$ar_role" in
          pre) echo "healthy"; return ;;
          post) echo "healthy"; return ;;
        esac
        if [ "$phase" = "Healthy" ]; then
          echo "healthy"
          return
        fi
        echo "pending"
        return
        ;;
      Pending|Running) echo "pending"; return ;;
    esac
  fi

  if [ "$phase" = "Healthy" ]; then
    echo "healthy"
    return
  fi
  echo "pending"
}

evaluate_command() {
  : "${ROLLOUT_JSON_FILE:?}"
  analysisrun_file="${ANALYSISRUN_JSON_FILE:-}"
  deployment_file="${DEPLOYMENT_JSON_FILE:-}"
  if [ -n "$analysisrun_file" ] && [ ! -f "$analysisrun_file" ]; then
    analysisrun_file=""
  fi
  if [ -n "$deployment_file" ] && [ ! -f "$deployment_file" ]; then
    deployment_file=""
  fi
  evaluate_rollout "$ROLLOUT_JSON_FILE" "$analysisrun_file" "$deployment_file"
}

approval_wait_command() {
  : "${APPROVAL_MARKER:?}"
  : "${APPROVAL_TIMEOUT:?}"
  waited=0
  while [ ! -f "$APPROVAL_MARKER" ]; do
    if [ "$waited" -ge "$APPROVAL_TIMEOUT" ]; then
      echo "approval-timeout"
      return 1
    fi
    sleep 5
    waited=$((waited + 5))
  done
  echo "approved"
}

fetch_rollout() {
  "$ROLLOUTS_CLI" get rollout "$(rollout_name)" --namespace "$(namespace)" --output json 2>/dev/null \
    | jq -c . > "$1"
}

fetch_analysisrun() {
  # $1: analysisrun name, $2: output file
  if [ -n "$1" ] && [ "$1" != "null" ]; then
    "$KUBECTL_CLI" get analysisrun "$1" --namespace "$(namespace)" -o json 2>/dev/null \
      | jq -c . > "$2"
  else
    rm -f "$2"
  fi
}

fetch_deployment() {
  "$KUBECTL_CLI" get deployment "$(resource_name)" --namespace "$(namespace)" -o json 2>/dev/null \
    | jq -c . > "$1"
}

wait_for_apply() {
  if [ -n "$FIXTURE_MODE" ]; then
    return 0
  fi
  if [ -z "${EXPECTED_GIT_SHA:-}" ]; then
    return 0
  fi
  attempt=0
  observed_sha=""
  while [ "$attempt" -lt 120 ]; do
    if [ "$(workload_kind)" = "Deployment" ]; then
      observed_sha=$("$KUBECTL_CLI" get deployment "$(resource_name)" --namespace "$(namespace)" -o json 2>/dev/null | \
        jq -r '.spec.template.metadata.labels["delivery.platform/git-sha"] // empty')
    else
      observed_sha=$("$ROLLOUTS_CLI" get rollout "$(rollout_name)" --namespace "$(namespace)" --output json 2>/dev/null | \
        jq -r '.spec.template.metadata.labels["delivery.platform/git-sha"] // empty')
    fi
    if [ "$observed_sha" = "$EXPECTED_GIT_SHA" ]; then
      return 0
    fi
    attempt=$((attempt + 1))
    sleep 5
  done
  echo "Argo CD did not apply $EXPECTED_GIT_SHA to $(resource_name) within 10 minutes" >&2
  return 1
}

wait_for_digest() {
  # $1: expected digest
  attempt=0
  while [ "$attempt" -lt 120 ]; do
    if [ "$(workload_kind)" = "Deployment" ]; then
      observed_image=$("$KUBECTL_CLI" get deployment "$(resource_name)" --namespace "$(namespace)" -o json 2>/dev/null | \
        jq -r '.spec.template.spec.containers[0].image // ""')
    else
      observed_image=$("$ROLLOUTS_CLI" get rollout "$(rollout_name)" --namespace "$(namespace)" --output json 2>/dev/null | \
        jq -r '.spec.template.spec.containers[0].image // ""')
    fi
    case "$observed_image" in
      *"@$1"*) return 0 ;;
    esac
    attempt=$((attempt + 1))
    sleep 5
  done
  echo "Argo CD did not apply digest $1 to $(resource_name) within 10 minutes" >&2
  return 1
}

wait_for_argocd() {
  # $1: application, $2: namespace, $3: expected revision
  attempt=0
  while [ "$attempt" -lt 120 ]; do
    app_json=$("$KUBECTL_CLI" get application "$1" --namespace "$2" -o json 2>/dev/null || true)
    revision=$(printf '%s' "$app_json" | jq -r '.status.sync.revision // ""')
    health=$(printf '%s' "$app_json" | jq -r '.status.health.status // ""')
    if [ "$revision" = "$3" ] && [ "$health" = "Healthy" ]; then
      return 0
    fi
    attempt=$((attempt + 1))
    sleep 5
  done
  echo "Argo CD Application $1 did not sync revision $3 (last revision=$revision health=$health)" >&2
  return 1
}

workload_serves_digest() {
  # $1: workload json file, $2: expected digest
  image=$(jq -r '.spec.template.spec.containers[0].image // ""' "$1")
  case "$image" in
    *"@$2"*) return 0 ;;
    *) return 1 ;;
  esac
}

evaluate_digest_command() {
  : "${EXPECTED_DIGEST:?}"
  if [ "$(workload_kind)" = "Deployment" ]; then
    : "${DEPLOYMENT_JSON_FILE:?}"
    if workload_serves_digest "$DEPLOYMENT_JSON_FILE" "$EXPECTED_DIGEST"; then
      echo "digest-ok"
      return 0
    fi
    echo "digest-mismatch" >&2
    return 1
  fi
  : "${ROLLOUT_JSON_FILE:?}"
  if workload_serves_digest "$ROLLOUT_JSON_FILE" "$EXPECTED_DIGEST"; then
    echo "digest-ok"
    return 0
  fi
  echo "digest-mismatch" >&2
  return 1
}

evaluate_argocd_command() {
  : "${ARGOCD_APPLICATION_JSON_FILE:?}"
  : "${CONFIG_REVISION:?}"
  revision=$(jq -r '.status.sync.revision // ""' "$ARGOCD_APPLICATION_JSON_FILE")
  health=$(jq -r '.status.health.status // ""' "$ARGOCD_APPLICATION_JSON_FILE")
  if [ "$revision" = "$CONFIG_REVISION" ] && [ "$health" = "Healthy" ]; then
    echo "argocd-ok"
    return 0
  fi
  echo "argocd-mismatch revision=$revision health=$health" >&2
  return 1
}

metrics_value() {
  # $1: key
  if [ -n "${FIXTURE_MODE:-}" ]; then
    : "${METRICS_JSON_FILE:?}"
    jq -r ".[\"$1\"] // \"\"" "$METRICS_JSON_FILE"
    return
  fi
  : "${PROMETHEUS_URL:?}"
  : "${KUBECTL_CLI:-}"
  case "$1" in
    samples)
      query=$(printf 'sum(rate(ecampus_http_requests_total{namespace=~"%s",service=~"%s"}[5m])) * 60' \
        "$(namespace)" "$(service_json '.service')")
      ;;
    error_rate)
      query=$(printf 'ecampus:http_error_ratio:rate5m{namespace=~"%s",service=~"%s"}' \
        "$(namespace)" "$(service_json '.service')")
      ;;
    p95)
      query=$(printf 'ecampus:http_request_duration_seconds:p95:5m{namespace=~"%s",service=~"%s"}' \
        "$(namespace)" "$(service_json '.service')")
      ;;
  esac
  curl --fail --silent --show-error --connect-timeout 5 --max-time 10 \
    -G "$PROMETHEUS_URL/api/v1/query" --data-urlencode "query=$query" \
    | jq -r '.data.result[0].value[1] // ""'
}

verify_metrics_command() {
  : "${SERVICE_JSON_FILE:?}"
  min_samples=$(service_json '.analysis.minSamples // 0')
  max_error=$(service_json '.analysis.maxErrorRate // 999')
  max_p95=$(service_json '.analysis.maxP95Seconds // 999')
  budget=${METRICS_OBSERVATION_SECONDS:-300}
  poll=${METRICS_POLL_SECONDS:-30}
  waited=0
  while [ "$waited" -lt "$budget" ]; do
    samples=$(metrics_value samples)
    error_rate=$(metrics_value error_rate)
    p95=$(metrics_value p95)
    if [ -n "$samples" ] && [ -n "$error_rate" ] && [ -n "$p95" ] &&
       [ "$(printf '%s' "$samples" | cut -d. -f1)" -ge "$min_samples" ] &&
       awk -v v="$error_rate" -v limit="$max_error" 'BEGIN{exit !(v <= limit)}' &&
       awk -v v="$p95" -v limit="$max_p95" 'BEGIN{exit !(v <= limit)}'; then
      echo "metrics healthy samples=$samples error_rate=$error_rate p95=$p95"
      return 0
    fi
    waited=$((waited + poll))
    if [ "$waited" -lt "$budget" ]; then
      sleep "$poll"
    fi
  done
  echo "post-release metrics never met SLI (last samples=$samples error_rate=$error_rate p95=$p95)" >&2
  return 1
}

wait_command() {
  : "${SERVICE_JSON_FILE:?}"
  : "${ROLLOUTS_CLI:?}"

  if [ "$(workload_kind)" = "Deployment" ]; then
    wait_for_apply
    "$KUBECTL_CLI" rollout status deployment/"$(resource_name)" --namespace "$(namespace)" --timeout=600s
    echo "healthy deployment $(resource_name)"
    return 0
  fi

  if [ -n "$FIXTURE_MODE" ]; then
    ROLLOUT_JSON_FILE="${ROLLOUT_JSON_FILE:?}"
  fi

  wait_for_apply

  if [ -n "${EXPECTED_DIGEST:-}" ]; then
    wait_for_digest "$EXPECTED_DIGEST"
  fi

  if [ -n "${ARGOCD_APP:-}" ] && [ -n "${CONFIG_REVISION:-}" ] && [ -z "${FIXTURE_MODE:-}" ]; then
    wait_for_argocd "$ARGOCD_APP" "${ARGOCD_NAMESPACE:-argocd}" "$CONFIG_REVISION"
  fi

  budget=$(seconds_from_duration "$(wait_timeout)")
  waited=0
  hold_started=0
  inconclusive_timeout=$(service_json '.analysis.inconclusive_timeout // "0s"')
  inconclusive_deadline=$(seconds_from_duration "$inconclusive_timeout")

  analysisrun_file=""
  deployment_file=""
  while :; do
    if [ -n "$FIXTURE_MODE" ]; then
      rollout_file="$ROLLOUT_JSON_FILE"
      ar_role=auto
      if [ -n "${ANALYSISRUN_JSON_FILE:-}" ] && [ -f "$ANALYSISRUN_JSON_FILE" ]; then
        analysisrun_file="$ANALYSISRUN_JSON_FILE"
      fi
      if [ -n "${DEPLOYMENT_JSON_FILE:-}" ] && [ -f "$DEPLOYMENT_JSON_FILE" ]; then
        deployment_file="$DEPLOYMENT_JSON_FILE"
      fi
    else
      rollout_file=$(mktemp)
      fetch_rollout "$rollout_file"
      ar_name=$(jq -r '.status.canary.currentStepAnalysisRun // .status.blueGreen.prePromotionAnalysisRun // .status.blueGreen.postPromotionAnalysisRun // ""' "$rollout_file")
      if [ "$ar_name" = "" ]; then
        ar_role=auto
      elif [ "$(jq -r '.status.canary.currentStepAnalysisRun // ""' "$rollout_file")" != "" ]; then
        ar_role=step
      elif [ "$(jq -r '.status.blueGreen.prePromotionAnalysisRun // ""' "$rollout_file")" != "" ]; then
        ar_role=pre
      else
        ar_role=post
      fi
      analysisrun_file=$(mktemp)
      fetch_analysisrun "$ar_name" "$analysisrun_file"
      deployment_file=$(mktemp)
      fetch_deployment "$deployment_file"
    fi

    verdict=$(evaluate_rollout "$rollout_file" "$analysisrun_file" "${deployment_file:-}" "$ar_role")
    case "$verdict" in
      healthy)
        echo "healthy $(rollout_name)"
        return 0
        ;;
      failed)
        echo "failed $(rollout_name)" >&2
        abort_release
        return 1
        ;;
      error|inconclusive)
        if [ "$inconclusive_deadline" -eq 0 ]; then
          echo "$verdict $(rollout_name); profile aborts immediately" >&2
          abort_release
          return 1
        fi
        if [ "$hold_started" -eq 0 ]; then
          hold_started=$waited
        fi
        if [ "$((waited - hold_started))" -ge "$inconclusive_deadline" ]; then
          echo "$verdict $(rollout_name) held for $inconclusive_deadline seconds; aborting" >&2
          abort_release
          return 1
        fi
        ;;
      pending) ;;
    esac

    if [ "$waited" -ge "$budget" ]; then
      echo "timeout waiting for $(rollout_name) after $budget seconds" >&2
      abort_release
      return 1
    fi
    sleep 10
    waited=$((waited + 10))
  done
}

# Promote the blue-green rollout after approval, then wait for full replica
# availability (promotion_timeout) and the post-promotion AnalysisRun.
promote_wait_command() {
  : "${SERVICE_JSON_FILE:?}"
  : "${ROLLOUTS_CLI:?}"

  "$ROLLOUTS_CLI" promote "$(rollout_name)" --namespace "$(namespace)"

  promotion_timeout=$(service_json '.promotion_timeout // "10m"')
  promotion_budget=$(seconds_from_duration "$promotion_timeout")
  waited=0
  while :; do
    rollout_file=$(mktemp)
    fetch_rollout "$rollout_file"
    desired=$(jq -r '.spec.replicas // 0' "$rollout_file")
    available=$(jq -r '.status.availableReplicas // 0' "$rollout_file")
    active_selector=$(jq -r '.status.blueGreen.activeSelector // ""' "$rollout_file")
    if [ -n "$active_selector" ] && [ "$desired" -gt 0 ] && [ "$available" -ge "$desired" ]; then
      break
    fi
    if [ "$waited" -ge "$promotion_budget" ]; then
      echo "blue-green promotion of $(rollout_name) did not reach full replicas within $promotion_budget seconds" >&2
      abort_release
      return 1
    fi
    sleep 10
    waited=$((waited + 10))
  done

  budget=$(seconds_from_duration "$(wait_timeout)")
  waited=0
  while :; do
    rollout_file=$(mktemp)
    fetch_rollout "$rollout_file"
    ar_name=$(jq -r '.status.blueGreen.postPromotionAnalysisRun // ""' "$rollout_file")
    analysisrun_file=$(mktemp)
    fetch_analysisrun "$ar_name" "$analysisrun_file"
    verdict=$(evaluate_rollout "$rollout_file" "$analysisrun_file" "" post)
    case "$verdict" in
      healthy)
        echo "post-promotion analysis of $(rollout_name) successful"
        return 0
        ;;
      failed|error)
        echo "$verdict post-promotion analysis of $(rollout_name)" >&2
        abort_release
        return 1
        ;;
      inconclusive)
        echo "inconclusive post-promotion analysis of $(rollout_name)" >&2
        abort_release
        return 1
        ;;
    esac
    if [ "$waited" -ge "$budget" ]; then
      echo "timeout waiting for post-promotion analysis of $(rollout_name)" >&2
      abort_release
      return 1
    fi
    sleep 10
    waited=$((waited + 10))
  done
}

case "${1:-}" in
  wait) wait_command ;;
  evaluate) evaluate_command ;;
  evaluate-digest) evaluate_digest_command ;;
  evaluate-argocd) evaluate_argocd_command ;;
  verify-metrics) verify_metrics_command ;;
  approval-wait) approval_wait_command ;;
  promote-wait) promote_wait_command ;;
  *) echo "usage: wait-for-release.sh wait|evaluate|evaluate-digest|evaluate-argocd|verify-metrics|approval-wait|promote-wait" >&2; exit 2 ;;
esac
