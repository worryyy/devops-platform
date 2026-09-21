#!/usr/bin/env sh
# wait-for-release.sh — wait for a single service Deployment to become healthy
# without growing the Jenkins Groovy. Parses Deployment and Argo CD Application
# JSON directly.
#
# Subcommands:
#   wait            poll the cluster until the Deployment is healthy
#   evaluate        pure verdict function over a local Deployment JSON (fixture tests)
#   evaluate-digest pure digest assertion over a local Deployment JSON
#   evaluate-argocd pure Argo CD sync/health assertion over local JSON
#   verify-metrics  poll Prometheus until the released service meets its SLI
#
# Verdicts:
#   healthy | failed | pending | timeout
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
#   METRICS_MIN_SAMPLES         default 20
#   METRICS_MAX_ERROR_RATE      default 0.02
#   KUBECTL_CLI            kubectl binary
#   FIXTURE_MODE=1         read JSON from local fixture files
#   DEPLOYMENT_JSON_FILE   fixture Deployment JSON
#   ARGOCD_APPLICATION_JSON_FILE fixture Application JSON
#   METRICS_JSON_FILE      fixture {"samples":..,"error_rate":..,"p95":..}

set -eu

WAIT_SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

service_json() {
  jq -r "$1" "$SERVICE_JSON_FILE"
}

resource_name() { service_json '.resource_name'; }
namespace() { service_json '.namespace'; }
wait_timeout() { service_json '.wait_timeout'; }
stable_service() { service_json '.stable_service'; }
health_path() { service_json '.health_path'; }
max_p95_seconds() { service_json '.max_p95_seconds'; }

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

evaluate_deployment() {
  # $1: deployment json file
  deployment_json="$1"
  if [ "$(jq -r '.status.conditions[]? | select(.type == "Progressing") | .reason' "$deployment_json")" = "ProgressDeadlineExceeded" ]; then
    echo "failed"
    return
  fi
  if [ "$(jq -r '.status.availableReplicas // 0' "$deployment_json")" -ge "$(jq -r '.status.replicas // 0' "$deployment_json")" ] && [ "$(jq -r '.status.replicas // 0' "$deployment_json")" -gt 0 ]; then
    echo "healthy"
    return
  fi
  echo "pending"
}

evaluate_command() {
  : "${DEPLOYMENT_JSON_FILE:?}"
  evaluate_deployment "$DEPLOYMENT_JSON_FILE"
}

fetch_deployment() {
  "$KUBECTL_CLI" get deployment "$(resource_name)" --namespace "$(namespace)" -o json 2>/dev/null \
    | jq -c . > "$1"
}

wait_for_apply() {
  if [ -n "${FIXTURE_MODE:-}" ]; then
    return 0
  fi
  if [ -z "${EXPECTED_GIT_SHA:-}" ]; then
    return 0
  fi
  attempt=0
  observed_sha=""
  while [ "$attempt" -lt 120 ]; do
    observed_sha=$("$KUBECTL_CLI" get deployment "$(resource_name)" --namespace "$(namespace)" -o json 2>/dev/null | \
      jq -r '.spec.template.metadata.labels["delivery.platform/git-sha"] // empty')
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
    observed_image=$("$KUBECTL_CLI" get deployment "$(resource_name)" --namespace "$(namespace)" -o json 2>/dev/null | \
      jq -r '.spec.template.spec.containers[0].image // ""')
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
  # $1: deployment json file, $2: expected digest
  image=$(jq -r '.spec.template.spec.containers[0].image // ""' "$1")
  case "$image" in
    *"@$2"*) return 0 ;;
    *) return 1 ;;
  esac
}

evaluate_digest_command() {
  : "${EXPECTED_DIGEST:?}"
  : "${DEPLOYMENT_JSON_FILE:?}"
  if workload_serves_digest "$DEPLOYMENT_JSON_FILE" "$EXPECTED_DIGEST"; then
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
  min_samples=${METRICS_MIN_SAMPLES:-20}
  max_error=${METRICS_MAX_ERROR_RATE:-0.02}
  max_p95=$(max_p95_seconds)
  if [ -z "$max_p95" ] || [ "$max_p95" = "null" ]; then
    max_p95=999
  fi
  budget=${METRICS_OBSERVATION_SECONDS:-300}
  poll=${METRICS_POLL_SECONDS:-30}
  waited=0
  while :; do
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
    if [ "$waited" -ge "$budget" ]; then
      echo "post-release metrics never met SLI (last samples=$samples error_rate=$error_rate p95=$p95)" >&2
      return 1
    fi
    waited=$((waited + poll))
    sleep "$poll"
  done
}

wait_command() {
  : "${SERVICE_JSON_FILE:?}"
  : "${KUBECTL_CLI:?}"

  wait_for_apply

  if [ -n "${EXPECTED_DIGEST:-}" ] && [ -z "${FIXTURE_MODE:-}" ]; then
    wait_for_digest "$EXPECTED_DIGEST"
  fi

  if [ -n "${ARGOCD_APP:-}" ] && [ -n "${CONFIG_REVISION:-}" ] && [ -z "${FIXTURE_MODE:-}" ]; then
    wait_for_argocd "$ARGOCD_APP" "${ARGOCD_NAMESPACE:-argocd}" "$CONFIG_REVISION"
  fi

  if [ -n "$FIXTURE_MODE" ]; then
    : "${DEPLOYMENT_JSON_FILE:?}"
    budget=$(seconds_from_duration "$(wait_timeout)")
    waited=0
    while :; do
      verdict=$(evaluate_deployment "$DEPLOYMENT_JSON_FILE")
      case "$verdict" in
        healthy)
          echo "healthy deployment $(resource_name)"
          return 0
          ;;
        failed)
          echo "failed deployment $(resource_name)" >&2
          return 1
          ;;
      esac
      if [ "$waited" -ge "$budget" ]; then
        echo "timeout waiting for $(resource_name) after $budget seconds" >&2
        return 1
      fi
      sleep 10
      waited=$((waited + 10))
    done
  fi

  "$KUBECTL_CLI" rollout status deployment/"$(resource_name)" --namespace "$(namespace)" --timeout=600s
  echo "healthy deployment $(resource_name)"
}

case "${1:-}" in
  wait) wait_command ;;
  evaluate) evaluate_command ;;
  evaluate-digest) evaluate_digest_command ;;
  evaluate-argocd) evaluate_argocd_command ;;
  verify-metrics) verify_metrics_command ;;
  *) echo "usage: wait-for-release.sh wait|evaluate|evaluate-digest|evaluate-argocd|verify-metrics" >&2; exit 2 ;;
esac
