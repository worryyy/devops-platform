import groovy.json.JsonSlurperClassic

def readJson(String path) {
  return parseJsonText(readFile(path))
}

// JsonSlurperClassic instances are not CPS-serializable; the pure parse stays
// in a @NonCPS method so the pipeline never checkpoints the parser itself.
@NonCPS
def parseJsonText(String text) {
  return new JsonSlurperClassic().parseText(text)
}

def csvContains(String csv, String service) {
  return (csv ?: '').split(',').collect { it.trim() }.contains(service)
}

def serviceKey(String service) {
  return service.toUpperCase().replaceAll(/[^A-Z0-9]/, '_')
}

def sourceMetadata(String service) {
  def impact = readJson('impact.json')
  def entries = (impact.test_matrix.include ?: []) + (impact.build_matrix.include ?: [])
  def matches = entries.findAll { it.service == service }
  if (!matches) {
    error('source metadata is missing for ' + service)
  }
  return matches[0]
}

def deliveryMetadata(String service) {
  def catalog = readJson('delivery-catalog.json')
  def matches = (catalog.services ?: []).findAll { it.service == service }
  if (matches.size() != 1) {
    error('delivery catalog must contain exactly one ' + service + ' entry')
  }
  return matches[0]
}

def imageDigest(String service) {
  return readFile(".ci/digests/${service}.digest").trim()
}

def rolloutStrategy(String service) {
  def delivery = deliveryMetadata(service)
  if (delivery.effective_profile == 'controlled-bluegreen') {
    return 'bluegreen'
  }
  if (delivery.effective_profile == 'fast-rolling') {
    return 'rolling'
  }
  return 'canary'
}

def recordRelease(String service, String status, String configRevision, String digestOverride = '', String gitRevisionOverride = '') {
  def digest = ''
  try {
    digest = digestOverride ?: imageDigest(service)
  } catch (err) {
    digest = ''
  }
  container('go') {
    withEnv([
      'SERVICE=' + service,
      'STATUS=' + status,
      'GIT_REVISION=' + (gitRevisionOverride ?: env.COMMIT_SHA),
      'DIGEST=' + digest,
      'CONFIG_REVISION=' + (configRevision ?: ''),
      'STRATEGY=' + rolloutStrategy(service),
    ]) {
      sh '''
        set -eu
        cd "$GITOPS_DIR/platform/server"
        go run ./cmd/server release-record \
          --service "$SERVICE" \
          --environment "$TARGET_ENV" \
          --record \
          --status "$STATUS" \
          --git-revision "$GIT_REVISION" \
          --image-digest "${DIGEST:-}" \
          --config-revision "${CONFIG_REVISION:-}" \
          --rollout-strategy "$STRATEGY"
      '''
    }
  }
}

def runServiceBranch(String service) {
  def source = sourceMetadata(service)
  def delivery = deliveryMetadata(service)
  def shouldBuild = csvContains(env.BUILD_SERVICES, service)

  if (delivery.image.tokenize('/').last() != source.image) {
    error('source image name and delivery repository disagree for ' + service)
  }

  container('go') {
    withEnv(['SERVICE=' + service]) {
      sh '''
        set -eu
        # the buildkitd sidecar (uid 1000) writes the image metadata here;
        # this container runs as root, so leave the dir world-writable
        mkdir -p "$WORKSPACE/.ci/digests" && chmod -R 777 "$WORKSPACE/.ci"
        if [ "${EXTREME_COLD:-false}" = "true" ]; then
          # baseline: no shared module/compile cache between services
          rm -rf /tmp/egc /tmp/egm
          export GOCACHE=/tmp/egc GOMODCACHE=/tmp/egm
        fi
        cd "$SOURCE_DIR"
        ./scripts/ci/run-service-checks.sh --service "$SERVICE"
      '''
    }
  }

  if (!shouldBuild) {
    return
  }

  container('buildkitd') {
    withEnv([
      'SERVICE=' + service,
      'SERVICE_PATH=' + source.entrypoint,
      'CONFIG_DIR=' + source.config_dir,
      'SERVICE_PORT=' + source.port,
      'DOCKERFILE=build/Dockerfile.go-service',
      'IMAGE=' + delivery.image,
      'CACHE_DIR=/home/user/.local/share/buildkit/local-cache',
    ]) {
      sh '''
        set -eu
        metadata="$WORKSPACE/.ci/digests/$SERVICE.json"
        import_cache=""
        if [ -d "$CACHE_DIR" ] && [ -n "$(ls -A "$CACHE_DIR" 2>/dev/null)" ]; then
          import_cache="--import-cache type=local,src=$CACHE_DIR"
        fi
        if [ "${EXTREME_COLD:-false}" = "true" ]; then
          # baseline: drop every reusable layer before the build
          buildctl prune --force >/dev/null 2>&1 || true
          import_cache=""
        fi
        buildctl build \
          --frontend=dockerfile.v0 \
          --local context="$WORKSPACE/$SOURCE_DIR" \
          --local dockerfile="$WORKSPACE/$SOURCE_DIR" \
          --opt filename="$DOCKERFILE" \
          --opt platform=linux/amd64 \
          --opt "build-arg:SERVICE_PATH=$SERVICE_PATH" \
          --opt "build-arg:CONFIG_DIR=$CONFIG_DIR" \
          --opt "build-arg:SERVICE_PORT=$SERVICE_PORT" \
          $import_cache \
          --export-cache "type=local,dest=$CACHE_DIR,mode=max" \
          --output "type=image,name=$IMAGE:$IMAGE_TAG,push=true" \
          --metadata-file="$metadata"
        test -s "$metadata"
      '''
    }
  }

  def imageMetadata = readJson(".ci/digests/${service}.json")
  def imageDigest = imageMetadata['containerimage.digest'] ?: ''
  if (!(imageDigest ==~ /^sha256:[0-9a-f]{64}$/)) {
    error('BuildKit returned an invalid image digest for ' + service)
  }
  writeFile(file: ".ci/digests/${service}.digest", text: imageDigest + '\n')

  recordRelease(service, 'releasing', '')
}

def writeDeliveryMetadata(String service) {
  def delivery = deliveryMetadata(service)
  writeFile(file: '.ci/delivery/' + service + '.json', text: groovy.json.JsonOutput.toJson(delivery))
}

def gitopsApi(String method, String apiPath, String body, String outFile) {
  def script = ''
  if (method == 'POST') {
    script = """
      set -eu
      mkdir -p "\$(dirname "\"\$WORKSPACE/${outFile}\"")"
      curl -fsS --retry 4 --retry-delay 5 --retry-all-errors -X POST \\
        -H "Authorization: Bearer \$GIT_TOKEN" \\
        -H "Accept: application/vnd.github+json" \\
        -H "Content-Type: application/json" \\
        --data-binary '$body' \\
        "https://api.github.com/repos/$apiPath" > "\$WORKSPACE/$outFile"
    """
  } else {
    script = """
      set -eu
      mkdir -p "\$(dirname "\"\$WORKSPACE/${outFile}\"")"
      curl -fsS --retry 4 --retry-delay 5 --retry-all-errors \\
        -H "Authorization: Bearer \$GIT_TOKEN" \\
        -H "Accept: application/vnd.github+json" \\
        "https://api.github.com/repos/$apiPath" > "\$WORKSPACE/$outFile"
    """
  }
  container('curl') {
    withCredentials([usernamePassword(credentialsId: 'git-https', usernameVariable: 'GIT_USER', passwordVariable: 'GIT_TOKEN')]) {
      sh script
    }
  }
  return readFile(outFile).trim()
}

def createGitOpsPR(String service, String branch, String title) {
  def body = groovy.json.JsonOutput.toJson([
    title: title,
    head: branch,
    base: 'main',
    body: 'Automated by ecampus-jenkins. Affected service: ' + service,
  ])
  def response = gitopsApi('POST', env.GITOPS_OWNER + '/' + env.GITOPS_REPO + '/pulls', body, ".ci/pr-${branch}.json")
  def pr = new JsonSlurperClassic().parseText(response)
  if (!pr.number) {
    error('GitHub did not return a pull request number')
  }
  echo 'GitOps PR created: ' + pr.html_url
  return [number: pr.number, nodeId: pr.node_id, url: pr.html_url]
}

def enableAutoMerge(String nodeId) {
  def query = 'mutation { enablePullRequestAutoMerge(input:{pullRequestId:\\"' + nodeId + '\\", mergeMethod: MERGE}) { clientMutationId } }'
  container('curl') {
    withCredentials([usernamePassword(credentialsId: 'git-https', usernameVariable: 'GIT_USER', passwordVariable: 'GIT_TOKEN')]) {
      sh """
        set -eu
        curl -fsS --retry 4 --retry-delay 5 --retry-all-errors -X POST \\
          -H "Authorization: Bearer \$GIT_TOKEN" \\
          -H "Content-Type: application/json" \\
          --data-binary '{"query": "$query"}' \\
          "https://api.github.com/graphql" >/dev/null
      """
    }
  }
}

def waitForPRMerged(int number, int timeoutSeconds) {
  def waited = 0
  while (waited < timeoutSeconds) {
    def response = gitopsApi('GET', env.GITOPS_OWNER + '/' + env.GITOPS_REPO + '/pulls/' + number, '', ".ci/pr-${number}.json")
    def pr = new JsonSlurperClassic().parseText(response)
    // GitHub reports merged PRs as state=closed with merged=true; state is
    // never the literal "merged" and the old check spun to the 900s timeout.
    if (pr.state == 'closed' && pr.merged) {
      echo 'GitOps PR ' + number + ' merged'
      return
    }
    sleep(time: 10, unit: 'SECONDS')
    waited += 10
  }
  error('GitOps PR ' + number + ' was not merged within ' + timeoutSeconds + ' seconds')
}

def gitopsRevisionAfterMerge() {
  container('git') {
    withCredentials([usernamePassword(credentialsId: 'git-https', usernameVariable: 'GIT_USER', passwordVariable: 'GIT_TOKEN')]) {
      sh '''
        set -eu
        clean_repo=$(echo "$GITOPS_REPO_URL" | sed 's#https://##')
        git ls-remote "https://$GIT_USER:$GIT_TOKEN@$clean_repo" refs/heads/main | awk '{print $1}' > "$WORKSPACE/gitops-head.txt"
      '''
    }
  }
  return readFile('gitops-head.txt').trim()
}

def patchServiceValues(String checkoutDir, String service, String digest, String tag, String configRevision) {
  def delivery = deliveryMetadata(service)
  def jobBase = env.JOB_BASE_NAME ?: 'ecampus-pipeline'
  def releaseBatch = (env.BRANCH_NAME ? jobBase + '-' + env.BRANCH_NAME : jobBase) + '-' + env.BUILD_NUMBER
  container('yq') {
    withEnv([
      'VALUES_FILE=' + checkoutDir + '/' + delivery.values_file,
      'DIGEST=' + digest,
      'TAG=' + tag,
      'GIT_SHA=' + env.COMMIT_SHA,
      'RELEASE_BATCH=' + releaseBatch,
      'DEPLOY_ID=' + releaseBatch + '-' + service + '-1',
      'PROFILE=' + (delivery.effective_profile ?: 'standard-canary'),
      'STRATEGY=' + rolloutStrategy(service),
      'AUTO_PROMOTION=' + (delivery.manual_promotion ? 'true' : 'false'),
      'PREVIEW_REPLICAS=' + (delivery.preview_replica_count ?: 0).toString(),
      'SCALE_DOWN_DELAY=' + (delivery.scale_down_delay_seconds ?: 900).toString(),
      'ANALYSIS_DRY_RUN=' + env.ANALYSIS_DRY_RUN,
      'ANALYSIS_INTERVAL=' + (delivery.analysis.interval ?: '1m'),
      'ANALYSIS_COUNT=' + (delivery.analysis.count ?: 10).toString(),
      'ANALYSIS_CONSECUTIVE=' + (delivery.analysis.consecutive_success_limit ?: 2).toString(),
      'ANALYSIS_FAILURE_LIMIT=' + (delivery.analysis.failure_limit ?: 2).toString(),
      'ANALYSIS_INCONCLUSIVE_LIMIT=' + (delivery.analysis.inconclusive_limit ?: 10).toString(),
      'ANALYSIS_CONSECUTIVE_ERROR_LIMIT=' + (delivery.analysis.consecutive_error_limit ?: 2).toString(),
      'ANALYSIS_MIN_SAMPLES=' + (delivery.analysis.min_samples ?: 1000).toString(),
      'ANALYSIS_STABLE_MIN_SAMPLES=' + (delivery.analysis.stable_min_samples ?: 1000).toString(),
      'ANALYSIS_MAX_ERROR_RATE=' + (delivery.analysis.max_error_rate ?: 0.02).toString(),
      'ANALYSIS_MAX_ERROR_INCREASE=' + (delivery.analysis.max_error_rate_increase ?: 0.01).toString(),
      'ANALYSIS_MAX_P95_RATIO=' + (delivery.analysis.max_p95_ratio ?: 1.5).toString(),
      'ANALYSIS_MAX_P95_SECONDS=' + (delivery.analysis.max_p95_seconds ?: 1.0).toString(),
      'ANALYSIS_MIN_OP_SUCCESS=' + (delivery.analysis.min_operation_success_rate ?: 0.99).toString(),
      'REQUEST_ROUTE_REGEX=' + (delivery.request_route_regex ?: ''),
      'OPERATION_ROUTE_REGEX=' + (delivery.operation_route_regex ?: ''),
      'CONFIG_REVISION=' + (configRevision ?: ''),
    ]) {
      sh '''
        set -eu
        yq eval -i '
          .image.digest = strenv(DIGEST) |
          .image.tag = strenv(TAG) |
          .release.releaseBatch = strenv(RELEASE_BATCH) |
          .release.deployId = strenv(DEPLOY_ID) |
          .release.gitSha = strenv(GIT_SHA) |
          .release.gitopsRevision = strenv(CONFIG_REVISION) |
          .release.environment = strenv(TARGET_ENV) |
          .release.buildNumber = strenv(BUILD_NUMBER) |
          .release.rolloutProfile = strenv(PROFILE) |
          .rollout.profile = strenv(PROFILE) |
          .rollout.strategy = strenv(STRATEGY) |
          .rollout.autoPromotion = (strenv(AUTO_PROMOTION) == "true") |
          .rollout.previewReplicaCount = (strenv(PREVIEW_REPLICAS) | tonumber) |
          .rollout.scaleDownDelaySeconds = (strenv(SCALE_DOWN_DELAY) | tonumber) |
          .rollout.analysis.dryRun = (strenv(ANALYSIS_DRY_RUN) == "true") |
          .rollout.analysis.interval = strenv(ANALYSIS_INTERVAL) |
          .rollout.analysis.count = (strenv(ANALYSIS_COUNT) | tonumber) |
          .rollout.analysis.consecutiveSuccessLimit = (strenv(ANALYSIS_CONSECUTIVE) | tonumber) |
          .rollout.analysis.failureLimit = (strenv(ANALYSIS_FAILURE_LIMIT) | tonumber) |
          .rollout.analysis.inconclusiveLimit = (strenv(ANALYSIS_INCONCLUSIVE_LIMIT) | tonumber) |
          .rollout.analysis.consecutiveErrorLimit = (strenv(ANALYSIS_CONSECUTIVE_ERROR_LIMIT) | tonumber) |
          .rollout.analysis.minSamples = (strenv(ANALYSIS_MIN_SAMPLES) | tonumber) |
          .rollout.analysis.stableMinSamples = (strenv(ANALYSIS_STABLE_MIN_SAMPLES) | tonumber) |
          .rollout.analysis.maxErrorRate = (strenv(ANALYSIS_MAX_ERROR_RATE) | tonumber) |
          .rollout.analysis.maxErrorRateIncrease = (strenv(ANALYSIS_MAX_ERROR_INCREASE) | tonumber) |
          .rollout.analysis.maxP95Ratio = (strenv(ANALYSIS_MAX_P95_RATIO) | tonumber) |
          .rollout.analysis.maxP95Seconds = (strenv(ANALYSIS_MAX_P95_SECONDS) | tonumber) |
          .rollout.analysis.minOperationSuccessRate = (strenv(ANALYSIS_MIN_OP_SUCCESS) | tonumber) |
          .rollout.analysis.requestRouteRegex = strenv(REQUEST_ROUTE_REGEX) |
          .rollout.analysis.operationRouteRegex = strenv(OPERATION_ROUTE_REGEX)
        ' "$VALUES_FILE"
      '''
    }
  }
}

def cloneGitOps(String checkoutDir) {
  container('git') {
    withCredentials([usernamePassword(credentialsId: 'git-https', usernameVariable: 'GIT_USER', passwordVariable: 'GIT_TOKEN')]) {
      withEnv(['CHECKOUT=' + checkoutDir, 'REPO_URL=' + env.GITOPS_REPO_URL]) {
        sh '''
          set -eu
          rm -rf "$CHECKOUT"
          clean_repo=$(echo "$REPO_URL" | sed 's#https://##')
          git clone --depth 1 "https://$GIT_USER:$GIT_TOKEN@$clean_repo" "$CHECKOUT"
          git -C "$CHECKOUT" remote set-url origin "$REPO_URL"
        '''
      }
    }
  }
}

def pushBranch(String checkoutDir, String branch) {
  container('git') {
    withCredentials([usernamePassword(credentialsId: 'git-https', usernameVariable: 'GIT_USER', passwordVariable: 'GIT_TOKEN')]) {
      withEnv(['CHECKOUT=' + checkoutDir, 'BRANCH=' + branch, 'REPO_URL=' + env.GITOPS_REPO_URL]) {
        sh '''
          set -eu
          clean_repo=$(echo "$REPO_URL" | sed 's#https://##')
          n=0
          until [ "$n" -ge 5 ]; do
            git -C "$CHECKOUT" push "https://$GIT_USER:$GIT_TOKEN@$clean_repo" HEAD:"$BRANCH" && break
            n=$((n+1)); sleep 20
          done
        '''
      }
    }
  }
}

def publishGitOps(String service, String branch, String digest, String tag, String title) {
  def checkout = 'gitops-' + service
  cloneGitOps(checkout)
  container('git') {
    withEnv(['CHECKOUT=' + checkout, 'BRANCH=' + branch]) {
      sh 'git -C "$CHECKOUT" checkout -b "$BRANCH" origin/main'
    }
  }
  // runs inside the git container: the jnlp user does not own the clone
  def baseRevision = container('git') {
    sh(script: 'git -C ' + checkout + ' rev-parse HEAD', returnStdout: true).trim()
  }
  patchServiceValues(checkout, service, digest, tag, baseRevision)
  container('git') {
    withEnv(['CHECKOUT=' + checkout, 'BRANCH=' + branch, 'TITLE=' + title]) {
      sh '''
        set -eu
        cd "$CHECKOUT"
        git config user.name "ecampus-jenkins"
        git config user.email "ecampus-jenkins@users.noreply.github.com"
        git add k3s/helm-values/workloads
        git diff --cached --quiet && { echo "expected GitOps values changes but index is empty" >&2; exit 1; }
        git commit -m "$TITLE"
      '''
    }
  }
  pushBranch(checkout, branch)
  return createGitOpsPR(service, branch, title)
}

def mergeGitOpsByRisk(String service, pr, boolean forceManual) {
  def delivery = deliveryMetadata(service)
  def manual = delivery.manual_promotion == true
  if (!manual && !forceManual) {
    enableAutoMerge(pr.nodeId)
    waitForPRMerged(pr.number, 900)
    return
  }
  try {
    input message: 'Approve GitOps PR for ' + service + ': ' + pr.url,
      submitterParameter: 'APPROVER',
      timeout: 30
  } catch (err) {
    echo 'GitOps PR approval timed out or was rejected for ' + service + '; release skipped'
    recordRelease(service, 'failed', '')
    throw err
  }
  waitForPRMerged(pr.number, 900)
}

def waitForRelease(String service, String configRevision, String digest = '', String expectedSha = '') {
  def delivery = deliveryMetadata(service)
  digest = digest ?: imageDigest(service)
  writeDeliveryMetadata(service)
  container('rollouts') {
    withEnv([
      'SERVICE_JSON_FILE=' + env.WORKSPACE + '/.ci/delivery/' + service + '.json',
      'EXPECTED_GIT_SHA=' + (expectedSha ?: env.COMMIT_SHA),
      'EXPECTED_DIGEST=' + digest,
      'CONFIG_REVISION=' + (configRevision ?: ''),
      'ARGOCD_APP=' + (delivery.application ?: ''),
      'ARGOCD_NAMESPACE=' + (delivery.argocd_namespace ?: 'argocd'),
      'WAIT_SCRIPT=' + env.GITOPS_DIR + '/k3s/ci/scripts/wait-for-release.sh',
    ]) {
      sh 'sh "$WAIT_SCRIPT" wait'
      sh '''
        set -eu
        if [ -n "${PROMETHEUS_URL:-}" ]; then
          sh "$WAIT_SCRIPT" verify-metrics
        fi
      '''
    }
  }

  try {
    container('curl') {
      withEnv([
        'STABLE_SERVICE=' + delivery.stable_service,
        'NAMESPACE=' + delivery.namespace,
        'HEALTH_PATH=' + delivery.health_path,
      ]) {
        sh '''
          set -eu
          curl --fail --silent --show-error \
            --retry 12 --retry-delay 5 --retry-all-errors \
            --connect-timeout 3 --max-time 8 \
            "http://$STABLE_SERVICE.$NAMESPACE.svc.cluster.local$HEALTH_PATH"
        '''
      }
    }
  } catch (err) {
    abortRelease(service)
    throw err
  }
  recordRelease(service, 'stable', configRevision, digest)
}

def abortRelease(String service) {
  def delivery = deliveryMetadata(service)
  container('rollouts') {
    withEnv(['ROLLOUT=' + delivery.rollout, 'NAMESPACE=' + delivery.namespace]) {
      sh '"$ROLLOUTS_CLI" abort "$ROLLOUT" --namespace "$NAMESPACE" || true'
    }
  }
}

def notifyFailure(String service, String digest, String prUrl) {
  if (!env.ALERTMANAGER_URL) {
    return
  }
  // Event-style alert with an explicit one-hour lifecycle: a single POST is
  // enough, Alertmanager resolves it after endsAt. Re-sending or a scraped
  // release-status metric is a future improvement and is not simulated here.
  def jobBase = env.JOB_BASE_NAME ?: 'ecampus-pipeline'
  def releaseBatch = (env.BRANCH_NAME ? jobBase + '-' + env.BRANCH_NAME : jobBase) + '-' + env.BUILD_NUMBER
  def startsAt = new Date().toInstant().toString()
  def endsAt = new Date(System.currentTimeMillis() + 3600000).toInstant().toString()
  def payload = groovy.json.JsonOutput.toJson([
    startsAt: startsAt,
    endsAt: endsAt,
    labels: [
      alertname: 'ReleaseFailed',
      service: service,
      environment: env.TARGET_ENV,
      deploy_id: releaseBatch + '-' + service + '-1',
    ],
    annotations: [digest: digest, pr: (prUrl ?: ''), job: env.JOB_NAME + '/' + env.BUILD_NUMBER],
  ])
  container('curl') {
    withEnv(['ALERTMANAGER_URL=' + env.ALERTMANAGER_URL, 'PAYLOAD=' + payload]) {
      sh 'curl -fsS -X POST "$ALERTMANAGER_URL/api/v2/alerts" --data-binary "[$PAYLOAD]" || true'
    }
  }
}

def rollbackRelease(String service, boolean undoRollout) {
  def checkout = 'gitops-' + service
  echo 'rolling back failed release for ' + service
  try {
    recordRelease(service, 'failed', '')
  } catch (err) {
    echo 'failed to record failed status for ' + service + ': ' + err
  }
  cloneGitOps(checkout)
  writeDeliveryMetadata(service)

  try {
    container('rollouts') {
      withEnv([
        'SERVICE_JSON_FILE=' + env.WORKSPACE + '/.ci/delivery/' + service + '.json',
        'GITOPS_DIR=' + env.WORKSPACE + '/' + checkout,
        'ROLLBACK_OUTPUT_DIR=' + env.WORKSPACE + '/.ci/rollback',
        'RELEASE_RECORD_BIN=/cache/jenkins-tools/platform-server release-record',
        'ROLLBACK_SCRIPT=' + env.GITOPS_DIR + '/k3s/ci/scripts/rollback-release.sh',
        'ROLLOUTS_CLI=' + env.ROLLOUTS_CLI,
        'KUBECTL_CLI=' + env.KUBECTL_CLI,
        'UNDO_ROLLOUT=' + (undoRollout ? '1' : '0'),
        'ROLLBACK_PAUSE_SYNC=' + (params.ROLLBACK_PAUSE_SYNC ? 'true' : 'false'),
        'SERVICE=' + service,
      ]) {
        sh '''
          set -eu
          sh "$ROLLBACK_SCRIPT" resolve-target
          export EXPECTED_DIGEST=$(jq -r '.image_digest' "$WORKSPACE/.ci/rollback/$SERVICE.json")
          if [ "$ROLLBACK_PAUSE_SYNC" = "true" ]; then
            sh "$ROLLBACK_SCRIPT" pause-selfheal
          fi
          sh "$ROLLBACK_SCRIPT" abort-traffic
          attempts=30
          while [ "$attempts" -gt 0 ]; do
            if sh "$ROLLBACK_SCRIPT" verify-traffic; then
              break
            fi
            attempts=$((attempts - 1))
            sleep 10
          done
          if [ "$attempts" -eq 0 ]; then
            echo "traffic did not return to the stable digest in time" >&2
            exit 1
          fi
        '''
      }
    }

    try {
      recordRelease(service, 'compensating', '')
    } catch (err) {
      echo 'failed to record compensating status for ' + service + ': ' + err
    }

    def target = readJson(".ci/rollback/${service}.json")
    def branch = 'rollback/' + service + '/' + env.SHORT_SHA
    def compensation = ''
    container('rollouts') {
      withEnv([
        'SERVICE_JSON_FILE=' + env.WORKSPACE + '/.ci/delivery/' + service + '.json',
        'GITOPS_DIR=' + env.WORKSPACE + '/' + checkout,
        'ROLLBACK_TARGET_FILE=' + env.WORKSPACE + '/.ci/rollback/' + service + '.json',
        'COMPENSATION_BRANCH=' + branch,
        'ROLLBACK_SCRIPT=' + env.GITOPS_DIR + '/k3s/ci/scripts/rollback-release.sh',
      ]) {
        compensation = sh(script: 'sh "$ROLLBACK_SCRIPT" prepare-compensation', returnStdout: true).trim()
      }
    }

    if (compensation.contains('COMPENSATION_SKIPPED=1')) {
      echo 'no compensation PR needed for ' + service
      if (target.source == 'git-history') {
        recordRelease(service, 'stable', target.config_revision ?: '', target.image_digest, target.git_revision ?: '')
      }
      return
    }

    pushBranch(checkout, branch)
    def pr = createGitOpsPR(service, branch, 'rollback(' + service + '): ' + (target.image_tag ?: 'stable'))
    mergeGitOpsByRisk(service, pr, false)
    def configRevision = gitopsRevisionAfterMerge()
    waitForRelease(service, configRevision, target.image_digest, target.git_revision ?: '')
    if (target.source == 'git-history') {
      recordRelease(service, 'stable', configRevision, target.image_digest, target.git_revision ?: '')
    }
  } finally {
    syncGuard(service, 'resume-selfheal')
  }
}

def promoteBlueGreen(String service) {
  def delivery = deliveryMetadata(service)
  writeDeliveryMetadata(service)
  container('rollouts') {
    withEnv([
      'SERVICE_JSON_FILE=' + env.WORKSPACE + '/.ci/delivery/' + service + '.json',
      'WAIT_SCRIPT=' + env.GITOPS_DIR + '/k3s/ci/scripts/wait-for-release.sh',
    ]) {
      sh 'sh "$WAIT_SCRIPT" promote-wait'
    }
  }
}

// Toggle automated.selfHeal on the service's Argo CD Application around a
// rollback: while the compensation PR is still open, Git keeps the failed
// revision and selfHeal would re-apply it over the live rollback (the
// rollback/self-heal race). The finally block in rollbackRelease always resumes.
def syncGuard(String service, String action) {
  if (params.ROLLBACK_PAUSE_SYNC == false) {
    return
  }
  container('rollouts') {
    withEnv([
      'SERVICE_JSON_FILE=' + env.WORKSPACE + '/.ci/delivery/' + service + '.json',
      'KUBECTL_CLI=' + env.KUBECTL_CLI,
      'ARGOCD_NAMESPACE=argocd',
      'ROLLBACK_SCRIPT=' + env.GITOPS_DIR + '/k3s/ci/scripts/rollback-release.sh',
    ]) {
      sh 'sh "$ROLLBACK_SCRIPT" ' + action
    }
  }
}

pipeline {
  agent {
    kubernetes {
      yaml """
apiVersion: v1
kind: Pod
spec:
  serviceAccountName: ecampus-release
  nodeSelector:
    platform-role: control
  securityContext:
    fsGroup: 1000
    fsGroupChangePolicy: OnRootMismatch
  imagePullSecrets:
    - name: tcr-secret
  containers:
    - name: go
      # baked with git/gcc/musl-dev/make/protobuf/jq and TUNA apk mirror
      image: crpi-gfwwpdquc14b7w22.cn-shanghai.personal.cr.aliyuncs.com/pulseops/golang-ci:1.26
      command: [cat]
      tty: true
      resources:
        requests:
          cpu: "1500m"
          memory: 2Gi
        limits:
          cpu: "3"
          memory: 8Gi
      env:
        - name: GOCACHE
          value: /cache/go-build
        - name: GOMODCACHE
          value: /cache/go-mod
        - name: GOPROXY
          value: https://goproxy.cn,https://mirrors.aliyun.com/goproxy/,direct
        - name: GODEBUG
          value: netdns=go
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: platform-postgresql-auth
              key: database-url
      volumeMounts:
        - name: jenkins-cache
          mountPath: /cache
    - name: buildkitd
      image: moby/buildkit:v0.31.2-rootless
      args:
        - --oci-worker-no-process-sandbox
      env:
        - name: DOCKER_CONFIG
          value: /home/user/.docker
      readinessProbe:
        exec:
          command: [buildctl, debug, workers]
        initialDelaySeconds: 5
        periodSeconds: 10
        timeoutSeconds: 10
        failureThreshold: 6
      # Under 4-way parallel builds buildctl can easily exceed the default 1s
      # exec timeout; a tight probe kills a healthy busy buildkitd and takes
      # the whole build down with it.
      livenessProbe:
        exec:
          command: [buildctl, debug, workers]
        initialDelaySeconds: 30
        periodSeconds: 30
        timeoutSeconds: 10
        failureThreshold: 6
      securityContext:
        runAsUser: 1000
        runAsGroup: 1000
        seccompProfile:
          type: Unconfined
        appArmorProfile:
          type: Unconfined
      volumeMounts:
        - name: buildkit-cache
          mountPath: /home/user/.local/share/buildkit
        - name: buildkit-config
          mountPath: /home/user/.config/buildkit
          readOnly: true
        - name: registry-secret
          mountPath: /home/user/.docker
          readOnly: true
      resources:
        requests:
          cpu: "500m"
          memory: 1Gi
        limits:
          cpu: "2"
          memory: 6Gi
    - name: git
      image: alpine/git:2.45.2
      command: [cat]
      tty: true
      resources:
        # The monorepo history contains large cache blobs; a full clone needs
        # several hundred MB of heap in git itself.
        requests:
          cpu: "100m"
          memory: 128Mi
        limits:
          cpu: "500m"
          memory: 1Gi
    - name: yq
      image: mikefarah/yq:4.44.3
      command: [cat]
      tty: true
      # patches files cloned by the root git container
      securityContext:
        runAsUser: 0
      resources:
        requests:
          cpu: "50m"
          memory: 64Mi
        limits:
          cpu: "200m"
          memory: 256Mi
    - name: rollouts
      # baked with jq/wget/curl/git/yq
      image: crpi-gfwwpdquc14b7w22.cn-shanghai.personal.cr.aliyuncs.com/pulseops/alpine-tools:3.21
      command: [cat]
      tty: true
      resources:
        requests:
          cpu: "100m"
          memory: 128Mi
        limits:
          cpu: "500m"
          memory: 512Mi
      env:
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: platform-postgresql-auth
              key: database-url
      volumeMounts:
        - name: jenkins-cache
          mountPath: /cache
    - name: curl
      image: curlimages/curl:8.10.1
      command: [cat]
      tty: true
      # the stock curl user (uid 100) cannot write the fsGroup-1000 workspace
      securityContext:
        runAsUser: 1000
        runAsGroup: 1000
      resources:
        requests:
          cpu: "50m"
          memory: 64Mi
        limits:
          cpu: "200m"
          memory: 256Mi
  volumes:
    - name: registry-secret
      secret:
        secretName: tcr-kaniko-secret
        items:
          - key: .dockerconfigjson
            path: config.json
    - name: buildkit-config
      configMap:
        name: buildkitd-config
    - name: buildkit-cache
      persistentVolumeClaim:
        claimName: buildkit-cache
    - name: jenkins-cache
      persistentVolumeClaim:
        claimName: jenkins-agent-cache
"""
    }
  }

  parameters {
    string(name: 'SOURCE_REPO', defaultValue: 'https://github.com/worryyy/app-test.git', description: 'Ecampus source repository.')
    string(name: 'TARGET_ENV', defaultValue: 'dev', description: 'Delivery catalog environment.')
    string(name: 'BEFORE_SHA', defaultValue: '', description: 'GitHub webhook before SHA; empty falls back to a conservative full build.')
    string(name: 'AFTER_SHA', defaultValue: '', description: 'GitHub webhook after SHA.')
    string(name: 'BUILDKIT_CACHE_TAG', defaultValue: 'main-amd64', description: 'BuildKit registry cache tag; point it at a never-used tag for cold-cache benchmark runs.')
    booleanParam(name: 'ROLLBACK_PAUSE_SYNC', defaultValue: true, description: 'Pause Argo CD selfHeal on the target Application during rollback; disable only to reproduce the rollback/self-heal race in drills.')
    booleanParam(name: 'SKIP_RELEASE', defaultValue: false, description: 'Stop after verify/build/push and skip the GitOps PR, rollout-wait and blue-green stages (CI benchmark mode).')
    booleanParam(name: 'EXTREME_COLD', defaultValue: false, description: 'Pre-optimization baseline mode: per-service ephemeral Go caches (no sharing across services) and a buildctl prune before every build (no layer reuse).')
  }

  environment {
    TARGET_ENV = "${params.TARGET_ENV ?: 'dev'}"
    SOURCE_DIR = 'source'
    GITOPS_DIR = 'gitops'
    GITOPS_REPO_URL = 'https://github.com/worryyy/app-test.git'
    GITOPS_OWNER = 'worryyy'
    GITOPS_REPO = 'app-test'
    BUILDKIT_CACHE_REPO = 'crpi-gfwwpdquc14b7w22.cn-shanghai.personal.cr.aliyuncs.com/pulseops'
    ROLLOUTS_CLI = '/cache/jenkins-tools/argo-rollouts/v1.8.3/kubectl-argo-rollouts'
    KUBECTL_CLI = '/cache/jenkins-tools/kubectl/v1.31.3/kubectl'
    ANALYSIS_DRY_RUN = 'false'
    PROMETHEUS_URL = 'http://prometheus.monitoring.svc:9090'
    ALERTMANAGER_URL = ''
  }

  options {
    skipDefaultCheckout(true)
    disableConcurrentBuilds(abortPrevious: false)
    timestamps()
  }

  stages {
    stage('Checkout main') {
      steps {
        container('git') {
          withCredentials([usernamePassword(credentialsId: 'git-https', usernameVariable: 'GIT_USER', passwordVariable: 'GIT_TOKEN')]) {
            sh '''
              set -eu
              rm -rf "$SOURCE_DIR" "$GITOPS_DIR" .ci impact.json delivery-catalog.json
              # GitHub auth-challenges anonymous git clones from AliCloud IPs,
              # so the public source repo uses the same read token as GitOps.
              source_repo="${SOURCE_REPO:-https://github.com/worryyy/app-test.git}"
              clean_source=$(echo "$source_repo" | sed 's#https://##')
              # waterfall analysis showed the full monorepo clone dominates
              # warm runs; a shallow window is enough for impact diffs and the
              # detect stage already falls back to --all when SHAs fall outside
              n=0; until [ "$n" -ge 3 ]; do git clone --depth 20 --branch main "https://$GIT_USER:$GIT_TOKEN@$clean_source" "$SOURCE_DIR" && break; n=$((n+1)); rm -rf "$SOURCE_DIR"; sleep 20; done

              clean_repo=$(echo "$GITOPS_REPO_URL" | sed 's#https://##')
              n=0; until [ "$n" -ge 3 ]; do git clone "https://$GIT_USER:$GIT_TOKEN@$clean_repo" "$GITOPS_DIR" && break; n=$((n+1)); rm -rf "$GITOPS_DIR"; sleep 20; done
              git -C "$GITOPS_DIR" remote set-url origin "$GITOPS_REPO_URL"
              test "$(git -C "$SOURCE_DIR" branch --show-current)" = main
              git -C "$SOURCE_DIR" rev-parse HEAD > current-head.txt
            '''
          }
        }
        script {
          env.COMMIT_SHA = readFile('current-head.txt').trim()
          env.SHORT_SHA = env.COMMIT_SHA.take(8)
          env.IMAGE_TAG = 'git-' + env.SHORT_SHA
        }
      }
    }

    stage('Detect affected services') {
      steps {
        container('go') {
          sh '''
            set -eu
            cd "$SOURCE_DIR"
            if [ -n "${BEFORE_SHA:-}" ] && [ -n "${AFTER_SHA:-}" ] &&
               git cat-file -e "$BEFORE_SHA^{commit}" 2>/dev/null &&
               git cat-file -e "$AFTER_SHA^{commit}" 2>/dev/null &&
               git merge-base --is-ancestor "$BEFORE_SHA" "$AFTER_SHA"; then
              go run ./cmd/ecampus-impact --base "$BEFORE_SHA" --head "$AFTER_SHA" > "$WORKSPACE/impact.json"
            else
              go run ./cmd/ecampus-impact --all > "$WORKSPACE/impact.json"
            fi
          '''
        }
        script {
          def impact = readJson('impact.json')
          def tests = (impact.test_matrix.include ?: []).collect { it.service }
          def builds = (impact.build_matrix.include ?: []).collect { it.service }
          env.TEST_SERVICES = tests.join(',')
          env.BUILD_SERVICES = builds.join(',')
          env.AFFECTED_SERVICES = (tests + builds).unique().join(',')
          env.REQUIRES_PROTO_CHECK = impact.requires_proto_check == true ? 'true' : 'false'
          echo 'test services: ' + (env.TEST_SERVICES ?: '(none)')
          echo 'build services: ' + (env.BUILD_SERVICES ?: '(none)')
          if (impact.fallback_reason) {
            echo 'conservative full build: ' + impact.fallback_reason
          }
        }
      }
    }

    stage('Resolve delivery catalog') {
      when {
        expression { return (env.AFFECTED_SERVICES ?: '').trim() }
      }
      steps {
        container('go') {
          withEnv(['REQUESTED_SERVICES=' + env.AFFECTED_SERVICES]) {
            sh '''
              set -eu
              cd "$GITOPS_DIR/platform/server"
              go run ./cmd/server catalog \
                --catalog configs/service-catalog.yaml \
                --services "$REQUESTED_SERVICES" \
                --environment "$TARGET_ENV" \
                > "$WORKSPACE/delivery-catalog.json"
            '''
          }
        }
        script {
          def catalog = readJson('delivery-catalog.json')
          def requested = env.AFFECTED_SERVICES.split(',') as List
          if ((catalog.services ?: []).size() != requested.size()) {
            error('delivery catalog did not return every requested service')
          }
          def bluegreen = (catalog.services ?: []).findAll { it.manual_promotion == true }.collect { it.service }
          env.BLUEGREEN_SERVICES = bluegreen.join(',')
          echo 'blue-green services: ' + (env.BLUEGREEN_SERVICES ?: '(none)')
        }
      }
    }

    stage('Verify agent generated code') {
      when {
        expression { return env.REQUIRES_PROTO_CHECK == 'true' }
      }
      steps {
        container('go') {
          sh '''
            set -eu
            go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
            go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
            cd "$SOURCE_DIR"
            make proto-agent
            git diff --exit-code -- proto/agent internal/agentchat/agentv1
          '''
        }
      }
    }

    stage('Verify, build and push') {
      when {
        expression { return (env.AFFECTED_SERVICES ?: '').trim() }
      }
      steps {
        script {
          // One go container hosts every service check, so the matrix runs in
          // batches: 13 parallel cgo compiles exceed the container memory
          // limit and thrash the node CPU anyway. Same batch size in every
          // benchmark run keeps the L0-L3 ladder comparable.
          def services = env.AFFECTED_SERVICES.split(',').findAll { it }
          // Sequential per-service builds: the local cache exporter is not
          // concurrency-safe for simultaneous writers on one directory, and
          // serial builds keep peak memory flat. Same shape in every
          // benchmark run keeps the L0-L3 ladder comparable.
          def batchSize = 1
          for (int i = 0; i < services.size(); i += batchSize) {
            def batch = services[i..Math.min(i + batchSize - 1, services.size() - 1)]
            def branches = [:]
            batch.each { service ->
              def current = service
              branches[current] = { runServiceBranch(current) }
            }
            parallel branches
          }
        }
      }
    }

    stage('Prepare tools') {
      steps {
        container('rollouts') {
          sh '''
            set -eu
            # jq/wget/curl/git/yq come from the baked alpine-tools image
            if [ ! -x "$ROLLOUTS_CLI" ]; then
              case "$(uname -m)" in
                x86_64) cli_layer=sha256:bf3ceff451710c15d85b84038cbabab49d132934a31e8edb5c436d7a3d972d04 ;;
                aarch64) cli_layer=sha256:608969b36e4770ccb572e6f815191cc39e393d3a4e95e0c1ef05ddbc81d31c53 ;;
                *) echo "unsupported rollout CLI architecture: $(uname -m)" >&2; exit 1 ;;
              esac
              tool_dir=$(dirname "$ROLLOUTS_CLI")
              mkdir -p "$tool_dir"
              layer_tmp=$(mktemp "$tool_dir/rollouts-layer.XXXXXX")
              extract_dir=$(mktemp -d "$tool_dir/rollouts-extract.XXXXXX")
              cleanup_rollouts_cli() {
                rm -f "$layer_tmp"
                rm -rf "$extract_dir"
              }
              trap cleanup_rollouts_cli EXIT
              wget -q \
                "https://quay.io/v2/argoproj/kubectl-argo-rollouts/blobs/$cli_layer" \
                -O "$layer_tmp"
              printf '%s  %s\n' "${cli_layer#sha256:}" "$layer_tmp" | sha256sum -c -
              tar -xzf "$layer_tmp" -C "$extract_dir" bin/kubectl-argo-rollouts
              chmod 0755 "$extract_dir/bin/kubectl-argo-rollouts"
              "$extract_dir/bin/kubectl-argo-rollouts" version
              mv "$extract_dir/bin/kubectl-argo-rollouts" "$ROLLOUTS_CLI"
              cleanup_rollouts_cli
              trap - EXIT
            fi
            "$ROLLOUTS_CLI" version
            if [ ! -x "$KUBECTL_CLI" ]; then
              kubectl_dir=$(dirname "$KUBECTL_CLI")
              mkdir -p "$kubectl_dir"
              kubectl_tmp=$(mktemp "$kubectl_dir/kubectl.XXXXXX")
              wget -q "https://dl.k8s.io/v1.31.3/bin/linux/amd64/kubectl" -O "$kubectl_tmp"
              wget -q "https://dl.k8s.io/v1.31.3/bin/linux/amd64/kubectl.sha256" -O "$kubectl_tmp.sha256"
              expected=$(cat "$kubectl_tmp.sha256")
              actual=$(sha256sum "$kubectl_tmp" | cut -d' ' -f1)
              test "$expected" = "$actual"
              chmod 0755 "$kubectl_tmp"
              mv "$kubectl_tmp" "$KUBECTL_CLI"
              rm -f "$kubectl_tmp.sha256"
            fi
            "$KUBECTL_CLI" version --client
          '''
        }
        container('go') {
          sh '''
            set -eu
            sed -i 's#https://dl-cdn.alpinelinux.org/alpine#https://mirrors.tuna.tsinghua.edu.cn/alpine#' /etc/apk/repositories 2>/dev/null || true
            mkdir -p /cache/jenkins-tools
            cd "$GITOPS_DIR/platform/server"
            go build -o /cache/jenkins-tools/platform-server ./cmd/server
            /cache/jenkins-tools/platform-server catalog --help >/dev/null 2>&1 || true  # go flag exits 2 on --help
            /cache/jenkins-tools/platform-server release-record --help >/dev/null 2>&1 || true
          '''
        }
      }
    }

    stage('Publish GitOps PRs') {
      when {
        allOf {
          expression { return (env.BUILD_SERVICES ?: '').trim() }
          expression { return params.SKIP_RELEASE != true }
        }
      }
      steps {
        script {
          def branches = [:]
          env.BUILD_SERVICES.split(',').findAll { it }.each { service ->
            def current = service
            branches[current] = {
              def branch = 'release/' + current + '/' + env.SHORT_SHA
              def pr = publishGitOps(current, branch, imageDigest(current), env.IMAGE_TAG, 'release(' + current + '): ' + env.IMAGE_TAG)
              mergeGitOpsByRisk(current, pr, false)
              def configRevision = gitopsRevisionAfterMerge()
              env['CONFIG_REV_' + serviceKey(current)] = configRevision
              recordRelease(current, 'releasing', configRevision)
            }
          }
          parallel branches
        }
      }
    }

    stage('Wait for rollout and health') {
      when {
        allOf {
          expression { return (env.BUILD_SERVICES ?: '').trim() }
          expression { return params.SKIP_RELEASE != true }
        }
      }
      steps {
        script {
          def branches = [:]
          env.BUILD_SERVICES.split(',').findAll { it }.each { service ->
            def current = service
            branches[current] = {
              def configRevision = env['CONFIG_REV_' + serviceKey(current)] ?: ''
              try {
                waitForRelease(current, configRevision)
              } catch (err) {
                echo 'release failed for ' + current + ': ' + err
                try {
                  rollbackRelease(current, false)
                } catch (rollbackErr) {
                  echo 'rollback failed for ' + current + ': ' + rollbackErr
                }
                notifyFailure(current, imageDigest(current), '')
                throw err
              }
            }
          }
          parallel branches
        }
      }
    }

    stage('Approve blue-green promotion') {
      when {
        allOf {
          expression { return (env.BLUEGREEN_SERVICES ?: '').trim() }
          expression { return params.SKIP_RELEASE != true }
        }
      }
      steps {
        script {
          try {
            input message: 'Approve blue-green promotion for: ' + env.BLUEGREEN_SERVICES,
              submitterParameter: 'APPROVER',
              timeout: 30
          } catch (err) {
            echo 'blue-green approval timed out or was rejected; aborting every pending blue-green release'
            env.BLUEGREEN_SERVICES.split(',').findAll { it }.each { abortRelease(it) }
            error('blue-green approval timeout or rejection')
          }
        }
      }
    }

    stage('Promote blue-green and wait post-promotion') {
      when {
        allOf {
          expression { return (env.BLUEGREEN_SERVICES ?: '').trim() }
          expression { return params.SKIP_RELEASE != true }
        }
      }
      steps {
        script {
          def branches = [:]
          env.BLUEGREEN_SERVICES.split(',').findAll { it }.each { service ->
            def current = service
            branches[current] = {
              try {
                promoteBlueGreen(current)
              } catch (err) {
                echo 'post-promotion failed for ' + current + ': ' + err
                try {
                  rollbackRelease(current, true)
                } catch (rollbackErr) {
                  echo 'rollback failed for ' + current + ': ' + rollbackErr
                }
                notifyFailure(current, imageDigest(current), '')
                throw err
              }
            }
          }
          parallel branches
        }
      }
    }
  }
}
