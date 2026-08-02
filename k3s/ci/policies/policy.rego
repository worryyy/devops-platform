package main

import future.keywords.if
import future.keywords.in

container_has_resources(container) if {
  container.resources.requests.cpu
  container.resources.requests.memory
  container.resources.limits.cpu
  container.resources.limits.memory
}

has_http_port(container) if {
  some port in container.ports
  port.name == "http"
  port.containerPort > 0
}

deny contains msg if {
  some doc in input
  doc.kind == "Deployment"
  some container in doc.spec.template.spec.containers
  not container_has_resources(container)
  msg := sprintf("container %s in %s has no resource requests/limits", [container.name, doc.metadata.name])
}

deny contains msg if {
  some doc in input
  doc.kind == "Rollout"
  some container in doc.spec.template.spec.containers
  not container_has_resources(container)
  msg := sprintf("container %s in %s has no resource requests/limits", [container.name, doc.metadata.name])
}

deny contains msg if {
  some doc in input
  doc.kind == "Deployment"
  some container in doc.spec.template.spec.containers
  endswith(container.image, ":latest")
  msg := sprintf("container %s in %s must not use the latest image tag", [container.name, doc.metadata.name])
}

deny contains msg if {
  some doc in input
  doc.kind == "Rollout"
  some container in doc.spec.template.spec.containers
  endswith(container.image, ":latest")
  msg := sprintf("container %s in %s must not use the latest image tag", [container.name, doc.metadata.name])
}

deny contains msg if {
  some doc in input
  doc.kind == "Deployment"
  some container in doc.spec.template.spec.containers
  regex.match(`^[^:]+:git-[^:@]+$`, container.image)
  msg := sprintf("container %s in %s uses a release git tag without a pinned digest", [container.name, doc.metadata.name])
}

deny contains msg if {
  some doc in input
  doc.kind == "Rollout"
  some container in doc.spec.template.spec.containers
  regex.match(`^[^:]+:git-[^:@]+$`, container.image)
  msg := sprintf("container %s in %s uses a release git tag without a pinned digest", [container.name, doc.metadata.name])
}

deny contains msg if {
  some doc in input
  doc.kind == "Deployment"
  some container in doc.spec.template.spec.containers
  container.securityContext.privileged == true
  msg := sprintf("container %s in %s must not be privileged", [container.name, doc.metadata.name])
}

deny contains msg if {
  some doc in input
  doc.kind == "Rollout"
  some container in doc.spec.template.spec.containers
  container.securityContext.privileged == true
  msg := sprintf("container %s in %s must not be privileged", [container.name, doc.metadata.name])
}

deny contains msg if {
  some doc in input
  doc.kind == "Deployment"
  doc.spec.template.spec.securityContext.privileged == true
  msg := sprintf("pod %s must not be privileged", [doc.metadata.name])
}

deny contains msg if {
  some doc in input
  doc.kind == "Rollout"
  doc.spec.template.spec.securityContext.privileged == true
  msg := sprintf("pod %s must not be privileged", [doc.metadata.name])
}

deny contains msg if {
  some doc in input
  doc.kind == "Deployment"
  some container in doc.spec.template.spec.containers
  not container.readinessProbe
  msg := sprintf("container %s in %s is missing readinessProbe", [container.name, doc.metadata.name])
}

deny contains msg if {
  some doc in input
  doc.kind == "Rollout"
  some container in doc.spec.template.spec.containers
  not container.readinessProbe
  msg := sprintf("container %s in %s is missing readinessProbe", [container.name, doc.metadata.name])
}

deny contains msg if {
  some doc in input
  doc.kind == "Deployment"
  some container in doc.spec.template.spec.containers
  not container.livenessProbe
  msg := sprintf("container %s in %s is missing livenessProbe", [container.name, doc.metadata.name])
}

deny contains msg if {
  some doc in input
  doc.kind == "Rollout"
  some container in doc.spec.template.spec.containers
  not container.livenessProbe
  msg := sprintf("container %s in %s is missing livenessProbe", [container.name, doc.metadata.name])
}

deny contains msg if {
  some doc in input
  doc.kind == "Deployment"
  some container in doc.spec.template.spec.containers
  not has_http_port(container)
  msg := sprintf("container %s in %s has no http container port", [container.name, doc.metadata.name])
}

deny contains msg if {
  some doc in input
  doc.kind == "Rollout"
  some container in doc.spec.template.spec.containers
  not has_http_port(container)
  msg := sprintf("container %s in %s has no http container port", [container.name, doc.metadata.name])
}

deny contains msg if {
  some doc in input
  doc.kind == "Service"
  some port in doc.spec.ports
  port.targetPort != "http"
  msg := sprintf("service %s port %v must target the http container port", [doc.metadata.name, port.port])
}

analysis_template_names contains name if {
  some doc in input
  doc.kind == "AnalysisTemplate"
  name := doc.metadata.name
}

deny contains msg if {
  some doc in input
  doc.kind == "Rollout"
  some step in doc.spec.strategy.canary.steps
  some template in step.analysis.templates
  not template.templateName in analysis_template_names
  msg := sprintf("rollout %s references missing AnalysisTemplate %s", [doc.metadata.name, template.templateName])
}

deny contains msg if {
  some doc in input
  doc.kind == "Rollout"
  some template in doc.spec.strategy.blueGreen.prePromotionAnalysis.templates
  not template.templateName in analysis_template_names
  msg := sprintf("rollout %s references missing AnalysisTemplate %s", [doc.metadata.name, template.templateName])
}

deny contains msg if {
  some doc in input
  doc.kind == "Rollout"
  some template in doc.spec.strategy.blueGreen.postPromotionAnalysis.templates
  not template.templateName in analysis_template_names
  msg := sprintf("rollout %s references missing AnalysisTemplate %s", [doc.metadata.name, template.templateName])
}
