k3s/
├── ci/
│   ├── jenkins/
│       ├── agent-cache-pvc.yaml
│       ├── buildkit-cache.yaml
│       ├── ecampus.Jenkinsfile
│       └── release-rbac.yaml
│   └── scripts/
│       ├── install-argocd.sh
│       └── test-delivery-contract.sh
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
│   │   ├── argo-rollouts.yaml
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
  (`release/<service>/<sha>`), updates `image.digest`/`tag` plus release and
  rollout values, and auto-merges normal services; services with
  `manual_promotion` or matched risk rules wait for human approval.
- The GitOps `deploy-gate` renders the final manifests with the PR values and
  validates them with kubeconform (Kubernetes 1.31 + bundled Argo Rollouts
  v1.8.3 CRD schemas) and conftest policies (`k3s/ci/policies/`): resource
  requests/limits, no `latest`, release `git-*` tags require a pinned digest,
  no privileged containers, health probes, Service/container port agreement
  and Rollout/AnalysisTemplate references.
- The built image digest flows into the GitOps values and the cluster
  workload. `wait-for-release.sh` asserts the Argo CD synced
  revision, the running image digest, rollout health, post-release Prometheus
  SLI and the stable service `/health` before the release is marked stable.
- Service-level release records (service, git_revision, image_digest,
  config_revision, rollout_strategy, release_status, released_at) are stored
  in PostgreSQL by `platform-server release-record`; the global Jenkins state
  file is gone. Rollback resolves the stable digest from the Rollout status,
  then the PostgreSQL stable record, then GitOps history; traffic is switched
  back first, and a compensation GitOps PR (`rollback/<service>/<sha>`)
  restores the desired state so Git and cluster agree.
- The release ServiceAccount is restricted to Rollout (abort/undo/promote),
  read-only workload access in the `app` namespace, and read-only Argo CD
  Application access in the `argocd` namespace.

## Release strategy baseline

Every service pins one static profile in
`platform/server/configs/service-catalog.yaml`; the effective strategy never
changes per release (no risk-rule escalation, no runtime re-selection):

| Profile | Strategy | Traffic / blast | Services |
|---|---|---|---|
| `critical-canary` | Canary `1% -> 5% -> 20% -> 50% -> 100%`, strict SLI | High traffic, high impact | comment, topic, user |
| `standard-canary` | Canary `20% -> 50% -> 100%`, standard SLI | Verifiable traffic, normal impact | academic, file |
| `controlled-bluegreen` | BlueGreen with Preview probes + manual approval | Low traffic or needs pre-shift verification | agentchat, chat, marketplace, moderation, notification, reservation, school |
| `fast-rolling` | Deployment (RollingUpdate), no analysis | Low traffic, low impact | theme |

Canary profiles gate progression on absolute SLI thresholds (error rate,
P95 latency, optional operation success rate) **and** relative regression
against the Stable revision, plus Stable-side health gates (sample count,
error rate, P95). Insufficient samples, Prometheus query failures or an
already-degraded Stable version pause the rollout; the pipeline aborts after
the profile's `analysis.inconclusive_timeout` (0s aborts immediately).
BlueGreen releases complete Preview probes against the Candidate Service and
require a human approval before promotion, then run post-promotion analysis.

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
`workload_release_info` / `deployment_release_info` recording rules and joined
into alerts with `group_left`, so a new release never creates new SLI series.

Alerts are classified with `signal_type`:

- `deploy_context`: noise window or analysis that could not produce a verdict;
  internal context signals that only drive inhibition and are routed to a
  config-less receiver, so they are never notified
- `release_gate_failed`: failed analysis or Degraded rollout (pre-traffic)
- `deploy_noise`: transient pod churn (inhibitable) and persistent or
  escalating failures (never inhibited)
- `user_impact`: version-level or service-level error rate / latency above
  thresholds, always with a >=50 requests/5m sample gate, plus an
  `alert_scope` label (`revision` for version-level, `service` or `ingress`
  otherwise)
- `infra`: platform storage alerts (for example Loki PVC usage)

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

### Loki and Alloy

The `loki` Argo CD Application installs (experimental, test/small-scale only):

- Loki chart `17.3.0` (Loki app version 3.7.2): Monolithic single replica,
  filesystem storage, 20Gi PVC, 7-day retention, NetworkPolicy-restricted
- Alloy chart `1.9.0` (Alloy app version v1.16.1): single-replica Deployment
  tailing pod logs through the Kubernetes API (no DaemonSet duplication; HA
  would require Alloy Clustering)

Index labels stay low-cardinality: `cluster/namespace/service/environment/
container`. Release identity is promoted into structured metadata from JSON
log fields (`deploy_id`, `release_batch`, `image_digest`, `git_sha`,
`gitops_revision`) and is also queryable with `| json`:

```logql
{namespace="app", service="comment", environment="dev"} | json | deploy_id="ecampus-pipeline-main-10-comment-1"
```

Chart versions and software versions are intentionally recorded separately:
the chart pins are fixed to the 2026-06-08 releases of both charts.
