k3s/
├── ci/
│   ├── jenkins/
│       ├── agent-cache-pvc.yaml
│       ├── buildkit-cache.yaml
│       ├── ecampus.Jenkinsfile
│       └── release-rbac.yaml
│   └── scripts/
│       ├── install-argocd.sh
│       ├── test-delivery-contract.sh
│       ├── test-observability.sh
│       ├── test-wait-release.sh
│       └── wait-for-release.sh
│
├── gitops/
│   ├── applications/
│   │   ├── delivery/
│   │   ├── platform/
│   │   └── workloads/
│   └── projects/
│
├── helm-values/
│   ├── delivery/
│   ├── platform/
│   │   ├── ingress-nginx.yaml
│   │   ├── platform-server.yaml
│   │   ├── prometheus.yaml
│   │   └── service-catalog.yaml
│   └── workloads/
│       ├── ecampus-academic.yaml
│       ├── ecampus-agentchat.yaml
│       ├── ecampus-chat.yaml
│       ├── ecampus-comment.yaml
│       ├── ecampus-file.yaml
│       ├── ecampus-marketplace.yaml
│       ├── ecampus-moderation.yaml
│       ├── ecampus-notification.yaml
│       ├── ecampus-reservation.yaml
│       ├── ecampus-school.yaml
│       ├── ecampus-theme.yaml
│       ├── ecampus-topic.yaml
│       └── ecampus-user.yaml
│
├── secrets/
│   └── tcr-secret.example.yaml
│
├── inventory/
├── playbooks/
└── roles/
│
This folder keeps the delivery configuration for the 13 Ecampus domain services:

- Ecampus-go owns Git diff and Go dependency-closure impact detection. Jenkins
  joins its service names with the platform delivery catalog, then verifies,
  builds and pushes affected services in parallel (BuildKit with
  per-service registry layer cache, Go module/compile cache on PVCs).
- PR gate: GitHub Actions runs `impact` / `go-test-build` / `golangci-lint`
  in Ecampus-go and `go-checks` / `golangci-lint` / `pipeline-scripts` /
  `helm-render` / `deploy-gate` in this repository; branch protection requires
  them before merging.
- After merge, Jenkins creates one GitOps PR per affected service
  (`release/<service>/<sha>`), updates `image.digest`/`tag` plus the release
  identity values, and enables GitHub auto-merge with a 15 minute merge
  deadline.
- The GitOps `deploy-gate` renders the final manifests with the PR values and
  validates them with kubeconform (Kubernetes 1.31) and conftest policies
  (`k3s/ci/policies/`): resource requests/limits, no `latest`, release `git-*`
  tags require a pinned digest, no privileged containers, health probes and
  Service/container port agreement.
- The built image digest flows into the GitOps values and the cluster
  workload. `wait-for-release.sh` asserts the Argo CD synced revision, the
  running image digest, Deployment rollout status, post-release Prometheus
  SLI and the stable service `/health` before the release is marked stable.
- Service-level release records (service, git_revision, image_digest,
  config_revision, release_status, released_at) are stored in PostgreSQL by
  `platform-server release-record`; the global Jenkins state file is gone.
  There is no automated rollback: a failed release is recorded as `failed`,
  pings Alertmanager (`ReleaseFailed`), and recovery is a `git revert` of the
  release commit followed by a normal re-release, which keeps Git and the
  cluster consistent by construction.
- The release ServiceAccount is read-only: workload access in the `app`
  namespace and Argo CD Application access in the `argocd` namespace, both
  limited to get/list/watch.

## Release strategy

Every service ships as a plain Deployment with RollingUpdate
(`maxUnavailable: 0`, `maxSurge: 1`): the pipeline keeps a single release
lane with no canary, blue-green or automated rollback machinery. Progressive
delivery was removed from this repository; if it is ever needed again, it
should come back as an explicit, separately reviewed change. Per-service SLI
budgets (`sli.maxP95Seconds`, route regexes) stay in the catalog and gate the
post-release metrics verification.

Before enabling the Ecampus Jenkins job, apply its persistent caches and release
permissions:

```shell
kubectl apply -f k3s/ci/jenkins/agent-cache-pvc.yaml
kubectl apply -f k3s/ci/jenkins/buildkit-cache.yaml
kubectl apply -f k3s/ci/jenkins/release-rbac.yaml
```

The Jenkins Go container uses `jenkins-agent-cache` for tests. Its rootless
BuildKit sidecar uses `buildkit-cache` for Go cache mounts and local build state,
while importing and exporting per-service build layers through TCR.

PostgreSQL release records require a `platform-postgresql-auth` Secret in the
`delivery` namespace (see `k3s/secrets/platform-postgresql-auth.example.yaml`)
with a `database-url` key, plus the PostgreSQL chart values in
`k3s/helm-values/platform/postgresql.yaml`. The migration in
`platform/server/migrations/001_release_records.sql` creates the
`service_releases` table with one stable row per service.

Install or upgrade public Argo CD with `ARGOCD_PUBLIC_HOST`,
`ARGOCD_TLS_SECRET`, and `ARGOCD_WEBHOOK_SECRET` set in the deployment
environment, then run `k3s/ci/scripts/install-argocd.sh`.
Configure the GitHub repository webhook URL as
`https://<ARGOCD_PUBLIC_HOST>/api/webhook` with the same webhook secret.

## Observability and release correlation

Every go-service workload carries release identity from CI/CD:

- Pod labels: `delivery.platform/deploy-id`,
  `delivery.platform/release-batch`, `delivery.platform/git-sha`,
  `delivery.platform/environment`
- Pod annotations: `delivery.platform/image-digest`,
  `delivery.platform/gitops-revision`
- Downward-API env vars: `DEPLOY_ID`, `RELEASE_BATCH`, `GIT_SHA`,
  `IMAGE_DIGEST`, `GITOPS_REVISION`

`release_batch` is `<JOB_BASE_NAME>-<BRANCH_NAME>-<BUILD_NUMBER>` (the
`<BRANCH_NAME>-` segment is omitted for non-multibranch jobs) and `deploy_id`
is `<release_batch>-<service>-<attempt>`; attempt is currently always `1`.

### Prometheus and Alertmanager

SLI recording rules (`ecampus:*`) stay low-cardinality: they are aggregated by
`namespace/service/environment/revision` only. Release identity is exposed by
the `delivery_platform:release_info` / `pod_release_info` /
`deployment_release_info` recording rules and joined
into alerts with `group_left`, so a new release never creates new SLI series.

Alerts are classified with `signal_type`:

- `deploy_context`: noise window; an internal context signal that only
  drives inhibition and is routed to a config-less receiver, so it is never
  notified
- `deploy_noise`: transient pod churn (inhibitable) and persistent or
  escalating failures (never inhibited)
- `user_impact`: version-level or service-level error rate / latency above
  thresholds, always with a >=50 requests/5m sample gate, plus an
  `alert_scope` label (`revision` for version-level, `service` or `ingress`
  otherwise)
- `infra`: platform storage alerts (inert example: `LokiPVCUsageHigh`; see
  the removal note under "Loki and Alloy")

Alertmanager inhibition is deliberately narrow: only `ReleasePodRestarting`
can be suppressed, by the release noise window or by a revision-scoped
`user_impact` alert (`alert_scope="revision"`) with the same non-empty
`namespace/service/environment/deploy_id`. Service- and ingress-level user
impact alerts never bind to a deploy and never act as inhibit sources.
`ReleasePodCrashLooping` (3+ restarts in 10 minutes) and
`ReleasePodStuckTerminating` (deletion timestamp older than 5 minutes) keep a
2 minute grace period and are never inhibited; `ReleasePodNotReady` and
`ReleaseReplicaShortage` keep a 5 minute grace period and are never inhibited
either. The noise window itself has no `for` grace period: as a pure context
signal it must be available for inhibition as soon as a release pod appears.

### Loki and Alloy (removed)

The experimental Loki + Alloy logging stack was removed from the platform:
none of the delivery-pipeline features depend on it, and the benchmark
cluster reserves node3 memory for the CI build pod. Alert annotations and the
`LokiPVCUsageHigh` rule remain as inert examples of the `infra` signal class
(they never fire without a Loki PVC); remove them together with their
promtool cases when consolidating the alert contract.
