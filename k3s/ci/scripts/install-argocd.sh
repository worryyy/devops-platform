#!/usr/bin/env sh
set -eu

: "${ARGOCD_PUBLIC_HOST:?ARGOCD_PUBLIC_HOST is required}"
: "${ARGOCD_TLS_SECRET:?ARGOCD_TLS_SECRET is required}"
: "${ARGOCD_WEBHOOK_SECRET:?ARGOCD_WEBHOOK_SECRET is required}"

repo_root=$(git rev-parse --show-toplevel)

helm repo add argo https://argoproj.github.io/argo-helm --force-update

helm upgrade --install argocd argo/argo-cd \
  --namespace argocd \
  --create-namespace \
  --values "${repo_root}/k3s/helm-values/delivery/argocd.yaml" \
  --set-string global.domain="${ARGOCD_PUBLIC_HOST}" \
  --set-string server.ingress.hostname="${ARGOCD_PUBLIC_HOST}" \
  --set server.ingress.tls=false \
  --set-string server.ingress.extraTls[0].hosts[0]="${ARGOCD_PUBLIC_HOST}" \
  --set-string server.ingress.extraTls[0].secretName="${ARGOCD_TLS_SECRET}" \
  --set-string configs.secret.githubSecret="${ARGOCD_WEBHOOK_SECRET}"
