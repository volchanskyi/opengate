package main

# A fully-compliant Deployment document used as the positive baseline; each
# negative test mutates exactly one field.
good_deployment := {
	"kind": "Deployment",
	"metadata": {"name": "server"},
	"spec": {"template": {"spec": {
		"securityContext": {"runAsNonRoot": true},
		"containers": [{
			"name": "server",
			"image": "ghcr.io/volchanskyi/opengate-server:v0.24.0",
			"resources": {"limits": {"cpu": "1", "memory": "512Mi"}},
			"livenessProbe": {"httpGet": {"path": "/healthz", "port": 8080}},
			"readinessProbe": {"httpGet": {"path": "/api/v1/health", "port": 8080}},
		}],
	}}},
}

# --- POSITIVE --------------------------------------------------------------

test_compliant_deployment_passes {
	count(deny) == 0 with input as good_deployment
}

test_container_level_non_root_passes {
	# runAsNonRoot set on the container instead of the pod also satisfies rule 3.
	doc := json.patch(good_deployment, [
		{"op": "remove", "path": "/spec/template/spec/securityContext"},
		{"op": "add", "path": "/spec/template/spec/containers/0/securityContext", "value": {"runAsNonRoot": true}},
	])
	count(deny) == 0 with input as doc
}

test_non_workload_doc_passes {
	# A Service has no containers → no rule fires.
	count(deny) == 0 with input as {"kind": "Service", "metadata": {"name": "server"}, "spec": {"ports": [{"port": 8080}]}}
}

test_cronjob_without_probes_passes {
	# Batch kinds are exempt from the probe rules but still need limits + non-root.
	doc := {
		"kind": "CronJob",
		"metadata": {"name": "pg-backup"},
		"spec": {"jobTemplate": {"spec": {"template": {"spec": {
			"securityContext": {"runAsNonRoot": true},
			"containers": [{
				"name": "backup",
				"image": "postgres:17-alpine",
				"resources": {"limits": {"cpu": "250m", "memory": "64Mi"}},
			}],
		}}}}},
	}
	count(deny) == 0 with input as doc
}

# --- NEGATIVE --------------------------------------------------------------

test_latest_tag_denied {
	doc := json.patch(good_deployment, [{"op": "add", "path": "/spec/template/spec/containers/0/image", "value": "postgres:latest"}])
	deny[msg] with input as doc
	contains(msg, "uses literal :latest")
}

test_untagged_image_denied {
	doc := json.patch(good_deployment, [{"op": "add", "path": "/spec/template/spec/containers/0/image", "value": "redis"}])
	deny[msg] with input as doc
	contains(msg, "has no tag")
}

test_missing_cpu_limit_denied {
	doc := json.patch(good_deployment, [{"op": "remove", "path": "/spec/template/spec/containers/0/resources/limits/cpu"}])
	deny[msg] with input as doc
	contains(msg, "no CPU limit")
}

test_missing_memory_limit_denied {
	doc := json.patch(good_deployment, [{"op": "remove", "path": "/spec/template/spec/containers/0/resources/limits/memory"}])
	deny[msg] with input as doc
	contains(msg, "no memory limit")
}

test_root_container_denied {
	doc := json.patch(good_deployment, [{"op": "remove", "path": "/spec/template/spec/securityContext"}])
	deny[msg] with input as doc
	contains(msg, "may run as root")
}

test_missing_liveness_probe_denied {
	doc := json.patch(good_deployment, [{"op": "remove", "path": "/spec/template/spec/containers/0/livenessProbe"}])
	deny[msg] with input as doc
	contains(msg, "no livenessProbe")
}

test_missing_readiness_probe_denied {
	doc := json.patch(good_deployment, [{"op": "remove", "path": "/spec/template/spec/containers/0/readinessProbe"}])
	deny[msg] with input as doc
	contains(msg, "no readinessProbe")
}
