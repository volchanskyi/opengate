# Each input is one rendered manifest document; documents without containers match no rule.

package main

workload_kinds := {"Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job"}

# Batch kinds are exempt from the health-probe rules.
longrunning_kinds := {"Deployment", "StatefulSet"}

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

# A pod-level runAsNonRoot satisfies the per-container requirement.
pod_run_as_non_root {
	workload_kinds[input.kind]
	input.spec.template.spec.securityContext.runAsNonRoot == true
}

pod_run_as_non_root {
	input.kind == "CronJob"
	input.spec.jobTemplate.spec.template.spec.securityContext.runAsNonRoot == true
}

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

deny[msg] {
	c := all_containers[_]
	not c.securityContext.runAsNonRoot == true
	not pod_run_as_non_root
	msg := sprintf("%v/%v: container %q may run as root — set securityContext.runAsNonRoot: true", [input.kind, input.metadata.name, c.name])
}

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
