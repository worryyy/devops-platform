package main

valid_deployment := {"kind": "Deployment", "metadata": {"name": "valid"}, "spec": {"template": {"spec": {"containers": [{
  "name": "app",
  "image": "crpi-gfwwpdquc14b7w22.cn-shanghai.personal.cr.aliyuncs.com/pulseops/app:git-abc12345@sha256:1111111111111111111111111111111111111111111111111111111111111111",
  "ports": [{"name": "http", "containerPort": 8080}],
  "readinessProbe": {"httpGet": {"path": "/health", "port": "http"}},
  "livenessProbe": {"httpGet": {"path": "/health", "port": "http"}},
  "resources": {"requests": {"cpu": "50m", "memory": "128Mi"}, "limits": {"cpu": "500m", "memory": "512Mi"}}
}]}}}}

container := valid_deployment.spec.template.spec.containers[0]

test_valid_deployment_has_no_denials if {
  result := deny with input as [valid_deployment]
  count(result) == 0
}

test_missing_resources_denied if {
  bad_container := {"name": "app", "image": "repo:tag@sha256:1111111111111111111111111111111111111111111111111111111111111111"}
  doc := {"kind": "Deployment", "metadata": {"name": "bad"}, "spec": {"template": {"spec": {"containers": [bad_container]}}}}
  result := deny with input as [doc]
  count(result) > 0
}

test_latest_image_denied if {
  latest := object.union(container, {"image": "repo:latest"})
  doc := {"kind": "Deployment", "metadata": {"name": "bad"}, "spec": {"template": {"spec": {"containers": [latest]}}}}
  result := deny with input as [doc]
  count(result) > 0
}

test_git_tag_without_digest_denied if {
  git_tag := object.union(container, {"image": "crpi-gfwwpdquc14b7w22.cn-shanghai.personal.cr.aliyuncs.com/pulseops/app:git-abc12345"})
  doc := {"kind": "Deployment", "metadata": {"name": "bad"}, "spec": {"template": {"spec": {"containers": [git_tag]}}}}
  result := deny with input as [doc]
  count(result) > 0
}

test_dev_tag_allowed if {
  dev := object.union(container, {"image": "crpi-gfwwpdquc14b7w22.cn-shanghai.personal.cr.aliyuncs.com/pulseops/app:dev"})
  doc := {"kind": "Deployment", "metadata": {"name": "dev"}, "spec": {"template": {"spec": {"containers": [dev]}}}}
  result := deny with input as [doc]
  count(result) == 0
}

test_missing_probe_denied if {
  no_probe := object.remove(container, ["readinessProbe"])
  doc := {"kind": "Deployment", "metadata": {"name": "bad"}, "spec": {"template": {"spec": {"containers": [no_probe]}}}}
  result := deny with input as [doc]
  count(result) > 0
}

test_privileged_denied if {
  privileged := object.union(container, {"securityContext": {"privileged": true}})
  doc := {"kind": "Deployment", "metadata": {"name": "bad"}, "spec": {"template": {"spec": {"containers": [privileged]}}}}
  result := deny with input as [doc]
  count(result) > 0
}

test_service_target_port_must_be_http if {
  doc := {"kind": "Service", "metadata": {"name": "svc"}, "spec": {"ports": [{"port": 80, "targetPort": 8080}]}}
  result := deny with input as [doc]
  count(result) > 0
}

test_missing_analysis_template_denied if {
  doc := {
    "kind": "Rollout",
    "metadata": {"name": "rollout"},
    "spec": {"strategy": {"canary": {"steps": [{"pause": {"duration": "1m"}}, {"analysis": {"templates": [{"templateName": "missing"}]}}]}}}
  }
  result := deny with input as [doc]
  count(result) > 0
}
