package deploy

// Rollback records are represented as deploy_records with deploy_type=rollback.
// The actual GitOps or Helm rollback executor can be added behind Service
// without changing HTTP route ownership.
