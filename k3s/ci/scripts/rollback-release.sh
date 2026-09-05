#!/usr/bin/env sh
# rollback-release.sh — rollback orchestration helpers for a failed release.
#
# Subcommands:
#   resolve-target         write .ci/rollback/<service>.json with the verified
#                          rollback target: L1 Argo Rollouts stableRS status,
#                          L2 PostgreSQL stable record, L3 GitOps git history.
#   abort-traffic          stop user traffic on the failing version (abort,
#                          undo, or kubectl rollout undo).
#   verify-traffic         assert the running workload serves the target digest.
#   prepare-compensation   create a GitOps branch that reverts one service's
#                          values back to the stable digest.
#   pause-selfheal         set the service Application's syncPolicy
#                          automated.selfHeal=false so Argo CD does not
#                          re-apply the failed revision over the live rollback
#                          while the compensation PR is still open.
#   resume-selfheal        restore automated.selfHeal=true once Git and the
#                          cluster agree again.
#
# Env inputs:
#   SERVICE_JSON_FILE      one entry of delivery-catalog.json
#   ROLLOUTS_CLI           kubectl-argo-rollouts binary
#   KUBECTL_CLI            kubectl binary
#   ARGOCD_NAMESPACE       default argocd (pause/resume-selfheal)
#   GITOPS_DIR             GitOps repository checkout
#   RELEASE_RECORD_BIN     release-record runner (default: go run ./cmd/server
#                          release-record inside PLATFORM_SERVER_DIR)
#   PLATFORM_SERVER_DIR    default platform/server
#   DATABASE_URL           PostgreSQL connection string (L2)
#   ROLLBACK_OUTPUT_DIR    default .ci/rollback
#   REGISTRY_HEAD_CMD      optional shell command verifying a digest still
#                          exists in the registry; receives REGISTRY_REPO and
#                          REGISTRY_DIGEST in the environment.
#   COMPENSATION_BRANCH    branch name for prepare-compensation
#   COMPENSATION_BASE_REF  default origin/main
#   DRY_RUN=1              print abort/undo commands instead of running them
#   FIXTURE_MODE=1         read JSON from ROLLOUT_JSON_FILE / STABLE_RS_JSON_FILE
#                          / CANARY_RS_JSON_FILE / DEPLOYMENT_JSON_FILE /
#                          STABLE_RECORD_JSON_FILE instead of the cluster

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROLLBACK_OUTPUT_DIR=${ROLLBACK_OUTPUT_DIR:-.ci/rollback}

service_json() {
  jq -r "$1" "$SERVICE_JSON_FILE"
}

service_name() { service_json '.service'; }
environment() { service_json '.environment // "dev"'; }
workload_kind() { service_json '.workload_kind // "Rollout"'; }
rollout_name() { service_json '.rollout'; }
resource_name() { service_json '.resource_name // .rollout'; }
namespace() { service_json '.namespace'; }
profile() { service_json '.effective_profile // "standard-canary"'; }
values_file() { service_json '.values_file'; }
image_repo() { service_json '.image'; }

digest_of() {
  printf '%s' "$1" | sed -n 's/.*@\(sha256:[0-9a-f]\{64\}\).*/\1/p'
}

tag_of() {
  printf '%s' "$1" | sed -n 's/.*:\([^:@]*\)@sha256:[0-9a-f]\{64\}.*/\1/p'
}

valid_digest() {
  printf '%s\n' "$1" | grep -Eq '^sha256:[0-9a-f]{64}$'
}

fetch_rollout() {
  if [ -n "${FIXTURE_MODE:-}" ]; then
    if [ -n "${ROLLOUT_JSON_FILE:-}" ]; then
      cp "$ROLLOUT_JSON_FILE" "$1"
      return
    fi
    return 1
  fi
  "$ROLLOUTS_CLI" get rollout "$(rollout_name)" --namespace "$(namespace)" --output json 2>/dev/null \
    | jq -c . > "$1"
}

fetch_replicaset() {
  # $1: output file, $2: replicaset name, $3: role (stable|canary|active)
  if [ -n "${FIXTURE_MODE:-}" ]; then
    case "$3" in
      stable) fixture_file="${STABLE_RS_JSON_FILE:-}" ;;
      canary) fixture_file="${CANARY_RS_JSON_FILE:-}" ;;
      active) fixture_file="${ACTIVE_RS_JSON_FILE:-}" ;;
      *) fixture_file="" ;;
    esac
    if [ -n "$fixture_file" ]; then
      cp "$fixture_file" "$1"
      return
    fi
    return
  fi
  "$KUBECTL_CLI" get rs "$2" --namespace "$(namespace)" -o json 2>/dev/null \
    | jq -c . > "$1" || rm -f "$1"
}

fetch_deployment() {
  if [ -n "${FIXTURE_MODE:-}" ]; then
    : "${DEPLOYMENT_JSON_FILE:?}"
    cp "$DEPLOYMENT_JSON_FILE" "$1"
    return
  fi
  "$KUBECTL_CLI" get deployment "$(resource_name)" --namespace "$(namespace)" -o json 2>/dev/null \
    | jq -c . > "$1"
}

write_target() {
  # $1: digest, $2: tag, $3: git revision, $4: config revision,
  # $5: rollout strategy, $6: source
  mkdir -p "$ROLLBACK_OUTPUT_DIR"
  output="$ROLLBACK_OUTPUT_DIR/$(service_name).json"
  jq -n \
    --arg service "$(service_name)" \
    --arg environment "$(environment)" \
    --arg digest "$1" \
    --arg tag "$2" \
    --arg git_revision "$3" \
    --arg config_revision "$4" \
    --arg rollout_strategy "$5" \
    --arg source "$6" \
    '{service: $service, environment: $environment, image_digest: $digest,
      image_tag: $tag, git_revision: $git_revision, config_revision: $config_revision,
      rollout_strategy: $rollout_strategy, source: $source}' > "$output"
  echo "rollback target: $1 (source: $6)" >&2
}

resolve_target_command() {
  : "${SERVICE_JSON_FILE:?}"
  : "${GITOPS_DIR:?}"
  service=$(service_name)
  env_name=$(environment)
  kind=$(workload_kind)

  digest=""
  tag=""
  git_revision=""
  config_revision=""
  strategy=""
  source=""

  # L1: Argo Rollouts controller status.
  if [ "$kind" = "Rollout" ]; then
    rollout_file=$(mktemp)
    if fetch_rollout "$rollout_file"; then
      stable_rs=$(jq -r '.status.stableRS // ""' "$rollout_file")
      active_selector=$(jq -r '.status.blueGreen.activeSelector // ""' "$rollout_file")
      if [ -n "$stable_rs" ]; then
        rs_file=$(mktemp)
        fetch_replicaset "$rs_file" "$stable_rs" stable
        if [ -s "$rs_file" ]; then
          image=$(jq -r '.spec.template.spec.containers[0].image // ""' "$rs_file")
          candidate=$(digest_of "$image")
          if valid_digest "$candidate"; then
            if [ -z "$active_selector" ] || case "$stable_rs" in *"-${active_selector}") true ;; *) false ;; esac; then
              digest=$candidate
              tag=$(tag_of "$image")
              strategy=$(jq -r '.metadata.labels["delivery.platform/rollout-profile"] // ""' "$rs_file")
              source="rollout-status"
            fi
          fi
        fi
      fi
    fi
  fi

  # L2: PostgreSQL verified stable record.
  if [ -z "$digest" ]; then
    record_file=$(mktemp)
    if [ -n "${FIXTURE_MODE:-}" ] && [ -n "${STABLE_RECORD_JSON_FILE:-}" ]; then
      cp "$STABLE_RECORD_JSON_FILE" "$record_file"
    else
      release_record_bin=${RELEASE_RECORD_BIN:-}
      if [ -z "$release_record_bin" ]; then
        server_dir=${PLATFORM_SERVER_DIR:-"$SCRIPT_DIR/../../../platform/server"}
        release_record_bin="go run ./cmd/server release-record"
        record_dir="$server_dir"
      else
        record_dir=$(pwd)
      fi
      if ! (cd "$record_dir" && sh -c "$release_record_bin --stable-digest --service \"$service\" --environment \"$env_name\"") > "$record_file" 2>/dev/null; then
        rm -f "$record_file"
      fi
    fi
    if [ -s "$record_file" ]; then
      candidate=$(jq -r '.image_digest // ""' "$record_file")
      if valid_digest "$candidate"; then
        digest=$candidate
        tag=$(jq -r '.image_tag // ""' "$record_file")
        git_revision=$(jq -r '.git_revision // ""' "$record_file")
        config_revision=$(jq -r '.config_revision // ""' "$record_file")
        strategy=$(jq -r '.rollout_strategy // ""' "$record_file")
        source="postgres"
        if [ -z "$tag" ] && [ -n "$git_revision" ]; then
          tag=$(printf 'git-%s' "$(printf '%s' "$git_revision" | cut -c1-8)")
        fi
      fi
    fi
  fi

  # L3: GitOps git history for the service values file.
  if [ -z "$digest" ]; then
    values=$(values_file)
    found=""
    for sha in $(git -C "$GITOPS_DIR" log --follow --format=%H -- "$values" 2>/dev/null | head -50); do
      candidate=$(git -C "$GITOPS_DIR" show "$sha:$values" 2>/dev/null | yq '.image.digest // ""' | tr -d '"')
      if ! valid_digest "$candidate"; then
        continue
      fi
      if [ -n "${REGISTRY_HEAD_CMD:-}" ]; then
        repo=$(git -C "$GITOPS_DIR" show "$sha:$values" 2>/dev/null | yq '.image.registry + "/" + .image.repository // ""' | tr -d '"')
        if REGISTRY_REPO="$repo" REGISTRY_DIGEST="$candidate" sh -c "$REGISTRY_HEAD_CMD" 2>/dev/null; then
          found="$sha"
          break
        fi
      else
        found="$sha"
        break
      fi
    done
    if [ -n "$found" ]; then
      values_content=$(git -C "$GITOPS_DIR" show "$found:$values")
      digest=$candidate
      config_revision=$found
      git_revision=$(printf '%s' "$values_content" | yq '.release.gitSha // ""' | tr -d '"')
      tag=$(printf '%s' "$values_content" | yq '.image.tag // ""' | tr -d '"')
      strategy=""
      source="git-history"
      if [ -z "$tag" ] && [ -n "$git_revision" ]; then
        tag=$(printf 'git-%s' "$(printf '%s' "$git_revision" | cut -c1-8)")
      fi
    fi
  fi

  if [ -z "$digest" ]; then
    echo "no rollback target found for $service (no stableRS, no stable record, no git history)" >&2
    return 3
  fi
  write_target "$digest" "$tag" "$git_revision" "$config_revision" "$strategy" "$source"
}

abort_traffic_command() {
  : "${SERVICE_JSON_FILE:?}"
  kind=$(workload_kind)
  if [ "$kind" = "Deployment" ] || [ "$(profile)" = "fast-rolling" ]; then
    cmd="$KUBECTL_CLI rollout undo deployment/$(resource_name) --namespace $(namespace)"
  elif [ "${UNDO_ROLLOUT:-0}" = "1" ]; then
    cmd="$ROLLOUTS_CLI undo $(rollout_name) --namespace $(namespace)"
  else
    cmd="$ROLLOUTS_CLI abort $(rollout_name) --namespace $(namespace)"
  fi
  if [ "${DRY_RUN:-0}" = "1" ]; then
    echo "$cmd"
    return 0
  fi
  sh -c "$cmd" || true
}

replicaset_serves_digest() {
  # $1: rs file, $2: expected digest
  [ -s "$1" ] || return 1
  image=$(jq -r '.spec.template.spec.containers[0].image // ""' "$1")
  case "$image" in
    *"@$2"*) return 0 ;;
    *) return 1 ;;
  esac
}

verify_traffic_command() {
  : "${SERVICE_JSON_FILE:?}"
  : "${EXPECTED_DIGEST:?}"
  digest=$EXPECTED_DIGEST
  kind=$(workload_kind)
  name=$(resource_name)
  ns=$(namespace)
  rollout=$(rollout_name)

  if [ "$kind" = "Deployment" ]; then
    deployment_file=$(mktemp)
    fetch_deployment "$deployment_file"
    image=$(jq -r '.spec.template.spec.containers[0].image // ""' "$deployment_file")
    replicas=$(jq -r '.status.replicas // 0' "$deployment_file")
    available=$(jq -r '.status.availableReplicas // 0' "$deployment_file")
    if case "$image" in *"@$digest"*) true ;; *) false ;; esac && [ "$available" -ge "$replicas" ] && [ "$replicas" -gt 0 ]; then
      echo "verified deployment $name serves $digest"
      return 0
    fi
    echo "deployment $name does not serve $digest (image=$image replicas=$replicas available=$available)" >&2
    return 1
  fi

  rollout_file=$(mktemp)
  fetch_rollout "$rollout_file"
  stable_rs=$(jq -r '.status.stableRS // ""' "$rollout_file")
  if [ -z "$stable_rs" ]; then
    echo "rollout $rollout has no stableRS" >&2
    return 1
  fi
  stable_rs_file=$(mktemp)
  fetch_replicaset "$stable_rs_file" "$stable_rs" stable
  if ! replicaset_serves_digest "$stable_rs_file" "$digest"; then
    echo "stable RS $stable_rs does not serve $digest" >&2
    return 1
  fi
  stable_replicas=$(jq -r '.spec.replicas // 0' "$stable_rs_file")
  stable_available=$(jq -r '.status.availableReplicas // 0' "$stable_rs_file")
  if [ "$stable_available" -lt "$stable_replicas" ] || [ "$stable_replicas" -le 0 ]; then
    echo "stable RS $stable_rs not fully available ($stable_available/$stable_replicas)" >&2
    return 1
  fi

  active_selector=$(jq -r '.status.blueGreen.activeSelector // ""' "$rollout_file")
  if [ -n "$active_selector" ]; then
    active_rs_file=$(mktemp)
    fetch_replicaset "$active_rs_file" "$rollout-$active_selector" active
    if ! replicaset_serves_digest "$active_rs_file" "$digest"; then
      echo "active RS does not serve $digest" >&2
      return 1
    fi
    echo "verified blue-green rollout $rollout serves $digest"
    return 0
  fi

  current_hash=$(jq -r '.status.currentPodHash // ""' "$rollout_file")
  if [ -n "$current_hash" ]; then
    canary_rs_file=$(mktemp)
    fetch_replicaset "$canary_rs_file" "$rollout-$current_hash" canary
    if [ -s "$canary_rs_file" ]; then
      canary_replicas=$(jq -r '.spec.replicas // 1' "$canary_rs_file")
      if [ "$canary_replicas" -ne 0 ]; then
        echo "canary RS $rollout-$current_hash still has $canary_replicas replicas" >&2
        return 1
      fi
    fi
  fi
  echo "verified canary rollout $rollout serves $digest"
}

prepare_compensation_command() {
  : "${SERVICE_JSON_FILE:?}"
  : "${GITOPS_DIR:?}"
  : "${COMPENSATION_BRANCH:?}"
  : "${ROLLBACK_TARGET_FILE:?}"
  base=${COMPENSATION_BASE_REF:-origin/main}
  values=$(values_file)
  target_digest=$(jq -r '.image_digest' "$ROLLBACK_TARGET_FILE")
  git_revision=$(jq -r '.git_revision // ""' "$ROLLBACK_TARGET_FILE")
  config_revision=$(jq -r '.config_revision // ""' "$ROLLBACK_TARGET_FILE")

  current_digest=$(git -C "$GITOPS_DIR" show "$base:$values" 2>/dev/null | yq '.image.digest // ""' | tr -d '"')
  if [ "$current_digest" = "$target_digest" ]; then
    echo "COMPENSATION_SKIPPED=1"
    echo "GitOps already points to the stable digest; no compensation PR needed" >&2
    return 0
  fi

  tag=$(jq -r '.image_tag // ""' "$ROLLBACK_TARGET_FILE")
  if [ -z "$tag" ]; then
    tag=$(printf 'git-%s' "$(printf '%s' "$git_revision" | cut -c1-8)")
  fi
  deploy_id=$(printf 'rollback-%s' "$(printf '%s' "$tag" | sed 's/^git-//' | cut -c1-8)")
  git -C "$GITOPS_DIR" checkout -B "$COMPENSATION_BRANCH" "$base"
  values_path="$GITOPS_DIR/$values"
  DIGEST="$target_digest" TAG="$tag" GIT_REVISION="$git_revision" \
  CONFIG_REV="$config_revision" DEPLOY_ID="$deploy_id" \
    yq eval -i '
      .image.digest = strenv(DIGEST) |
      .image.tag = strenv(TAG) |
      .release.gitSha = strenv(GIT_REVISION) |
      .release.gitopsRevision = strenv(CONFIG_REV) |
      .release.deployId = strenv(DEPLOY_ID)
    ' "$values_path"
  git -C "$GITOPS_DIR" add "$values"
  git -C "$GITOPS_DIR" -c user.name="${COMPENSATION_GIT_NAME:-ecampus-jenkins}" \
    -c user.email="${COMPENSATION_GIT_EMAIL:-ecampus-jenkins@users.noreply.github.com}" \
    commit -m "rollback($(service_name)): $tag"
  echo "COMPENSATION_BRANCH=$COMPENSATION_BRANCH"
  echo "COMPENSATION_VALUES_FILE=$values"
  echo "compensation branch $COMPENSATION_BRANCH reverts $(service_name) to $target_digest" >&2
}

selfheal_command() {
  # $1 = "true" (resume) | "false" (pause); patches only automated.selfHeal
  # because auto-sync itself is harmless here: Git has not changed, the race
  # is selfHeal re-applying the failed revision over the rolled-back state.
  app=$(service_json '.application // empty')
  if [ -z "$app" ]; then
    echo "service entry has no Argo CD application; nothing to $1 self-heal for" >&2
    return 0
  fi
  if [ "${DRY_RUN:-0}" = "1" ]; then
    echo "kubectl -n ${ARGOCD_NAMESPACE:-argocd} patch application ${app} --type=merge -p {\"spec\":{\"syncPolicy\":{\"automated\":{\"selfHeal\":$1}}}}"
    return 0
  fi
  "$KUBECTL_CLI" -n "${ARGOCD_NAMESPACE:-argocd}" patch application "$app" \
    --type=merge \
    -p "{\"spec\":{\"syncPolicy\":{\"automated\":{\"selfHeal\":$1}}}}"
}

pause_selfheal_command() { selfheal_command false; }
resume_selfheal_command() { selfheal_command true; }

case "${1:-}" in
  resolve-target) resolve_target_command ;;
  abort-traffic) abort_traffic_command ;;
  verify-traffic) verify_traffic_command ;;
  prepare-compensation) prepare_compensation_command ;;
  pause-selfheal) pause_selfheal_command ;;
  resume-selfheal) resume_selfheal_command ;;
  *) echo "usage: rollback-release.sh resolve-target|abort-traffic|verify-traffic|prepare-compensation|pause-selfheal|resume-selfheal" >&2; exit 2 ;;
esac
