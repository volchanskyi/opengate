# Kubernetes manifest policy (MVP) — runs against the rendered Helm output:
#   helm template … | conftest test -p policy/k8s -
#
# Today's rules (deny):
#   1. `image:` must not end in literal `:latest`, and must carry a tag.
#   2. Every workload container must declare CPU + memory resource limits
#      (Always-Free 4 OCPU / 24 GB budget discipline — an unbounded pod can
#      starve the single node).
#   3. Every workload container must run as non-root (pod- or container-level
#      securityContext.runAsNonRoot: true).
#   4. Long-running workloads (Deployment / StatefulSet) must declare both a
#      liveness and a readiness probe on every container.
#
# Input shape: a single rendered Kubernetes manifest document (kind/metadata/
# spec…). conftest feeds each document of the multi-doc stream in turn; docs
# without containers (Service, ConfigMap, Ingress, ClusterIssuer, …) match no
# rule and pass.

package main

# Workload kinds whose pod template lives at spec.template.spec.
workload_kinds := {"Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job"}

# Long-running kinds that must expose health probes (batch kinds are exempt).
longrunning_kinds := {"Deployment", "StatefulSet"}

# all_containers collects every app container across the kinds we deploy.
all_containers[c] {
	workload_kinds[input.kind]
	c := input.spec.template.spec.containers[_]
}

all_containers[c] {
	input.kind == "CronJob"
	c := input.spec.jobTemplate.spec.template.spec.containers[_]
}

all_containers[c] {
	input.kind == "Pod"
	c := input.spec.containers[_]
}

# pod_security_context resolves the pod-level securityContext for the kinds
# that carry a pod template (so a pod-level runAsNonRoot satisfies rule 3).
pod_run_as_non_root {
	workload_kinds[input.kind]
	input.spec.template.spec.securityContext.runAsNonRoot == true
}

pod_run_as_non_root {
	input.kind == "CronJob"
	input.spec.jobTemplate.spec.template.spec.securityContext.runAsNonRoot == true
}

# --- Rule 1: image tag hygiene ---------------------------------------------

deny[msg] {
	c := all_containers[_]
	endswith(c.image, ":latest")
	msg := sprintf("%v/%v: container %q image %q uses literal :latest — pin a concrete tag", [input.kind, input.metadata.name, c.name, c.image])
}

deny[msg] {
	c := all_containers[_]
	not contains(c.image, ":")
	msg := sprintf("%v/%v: container %q image %q has no tag — append :<version>", [input.kind, input.metadata.name, c.name, c.image])
}

# --- Rule 2: resource limits -----------------------------------------------

deny[msg] {
	c := all_containers[_]
	not c.resources.limits.cpu
	msg := sprintf("%v/%v: container %q has no CPU limit — set resources.limits.cpu", [input.kind, input.metadata.name, c.name])
}

deny[msg] {
	c := all_containers[_]
	not c.resources.limits.memory
	msg := sprintf("%v/%v: container %q has no memory limit — set resources.limits.memory", [input.kind, input.metadata.name, c.name])
}

# --- Rule 3: run as non-root -----------------------------------------------

deny[msg] {
	c := all_containers[_]
	not c.securityContext.runAsNonRoot == true
	not pod_run_as_non_root
	msg := sprintf("%v/%v: container %q may run as root — set securityContext.runAsNonRoot: true", [input.kind, input.metadata.name, c.name])
}

# --- Rule 4: health probes on long-running workloads -----------------------

deny[msg] {
	longrunning_kinds[input.kind]
	c := input.spec.template.spec.containers[_]
	not c.livenessProbe
	msg := sprintf("%v/%v: container %q has no livenessProbe", [input.kind, input.metadata.name, c.name])
}

deny[msg] {
	longrunning_kinds[input.kind]
	c := input.spec.template.spec.containers[_]
	not c.readinessProbe
	msg := sprintf("%v/%v: container %q has no readinessProbe", [input.kind, input.metadata.name, c.name])
}
