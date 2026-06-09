k3s-platform/
├── ansible.cfg
├── inventory/
│   ├── dev.ini
│   └── group_vars/
│       └── all.yml
│
├── playbooks/
│   ├── site.yml
│   ├── bootstrap.yml
│   ├── k3s-cluster.yml
│   ├── kubeconfig.yml
│   ├── verify.yml
│   └── app-deploy.yml
│
├── roles/
│   ├── common/
│   ├── k3s_server/
│   ├── k3s_agent/
│   ├── kubeconfig/
│   └── kube_verify/
│
├── apps/
│   └── api/
│       ├── Dockerfile
│       ├── .dockerignore
│       ├── go.mod
│       ├── go.sum
│       ├── cmd/
│       │   └── server/
│       │       └── main.go
│       └── internal/
│
├── charts/
│   └── forum-api/
│       ├── Chart.yaml
│       ├── values.yaml
│       └── templates/
│           ├── deployment.yaml
│           ├── service.yaml
│           ├── configmap.yaml
│           ├── hpa.yaml
│           └── _helpers.tpl
│
├── helm-values/
│   ├── workloads/
│   │   ├── forum-api-business.yaml
│   │   └── forum-api-target.yaml
│   │
│   ├── dependencies/
│   │   ├── mysql.yaml
│   │   └── mongodb.yaml
│   │
│   ├── observability/
│   │   ├── prometheus-stack.yaml
│   │   ├── loki.yaml
│   │   └── promtail.yaml
│   │
│   ├── delivery/
│   │   └── argocd.yaml
│   │
│   ├── chaos/
│   │   └── chaos-mesh.yaml
│   │
│   └── platform/
│       ├── ingress-nginx.yaml
│       └── cert-manager.yaml
│
├── gitops/
│   ├── bootstrap/
│   │   └── root-app.yaml
│   │
│   ├── projects/
│   │   ├── workloads.yaml
│   │   ├── observability.yaml
│   │   ├── delivery.yaml
│   │   ├── chaos.yaml
│   │   └── platform.yaml
│   │
│   └── applications/
│       ├── workloads/
│       │   ├── forum-api.yaml
│       │   └── forum-api-target.yaml
│       │
│       ├── observability/
│       │   ├── prometheus-stack.yaml
│       │   ├── loki.yaml
│       │   └── promtail.yaml
│       │
│       ├── delivery/
│       │   └── argocd-self.yaml
│       │
│       ├── chaos/
│       │   └── chaos-mesh.yaml
│       │
│       └── platform/
│           ├── ingress-nginx.yaml
│           └── cert-manager.yaml
├── ci/
│   └── jenkins/
│       └── forum-api.Jenkinsfile
├── secrets/
│   ├── .gitignore
│   ├── forum-api-secrets.example.yaml
│   ├── mysql-auth.example.yaml
│   ├── mongodb-auth.example.yaml
│   ├── forum-api-secrets.yaml
│   ├── mysql-auth.yaml
│   └── mongodb-auth.yaml
│
└── scripts/
    ├── build-image.sh
    ├── push-image.sh
    └── create-pull-secrets.sh

apps/
  放你的 Go 服务源码和 Dockerfile。

charts/api/
  放你自己业务服务的 Helm Chart。

helm-values/dependencies/
  放 MySQL、Redis 这种第三方 Helm Chart 的 values。

helm-values/observability/
  后面放 Prometheus、Loki、Promtail 的 values。

playbooks/deps.yml
  用 Ansible 调 Helm 安装 MySQL / Redis。

playbooks/app.yml
  用 Ansible 调 Helm 安装你的 Go 服务。