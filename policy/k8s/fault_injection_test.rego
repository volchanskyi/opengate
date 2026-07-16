package main

# A clean Ingress carrying only ordinary annotations is the positive baseline;
# the negative test adds a single fault-injection marker annotation.
clean_ingress := {
	"kind": "Ingress",
	"metadata": {
		"name": "opengate",
		"annotations": {"nginx.ingress.kubernetes.io/proxy-read-timeout": "3600"},
	},
	"spec": {"rules": [{"host": "opengate.example.com"}]},
}

# --- POSITIVE --------------------------------------------------------------

test_clean_ingress_passes {
	count(deny) == 0 with input as clean_ingress
}

test_resource_without_annotations_passes {
	# A Service carries no annotations → the fault rule cannot fire.
	count(deny) == 0 with input as {"kind": "Service", "metadata": {"name": "opengate-server"}, "spec": {"ports": [{"port": 8080}]}}
}

# --- NEGATIVE --------------------------------------------------------------

test_fault_scenario_annotation_denied {
	doc := json.patch(clean_ingress, [{"op": "add", "path": "/metadata/annotations/fault.opengate.dev~1scenario", "value": "edge-504-timeout"}])
	deny[msg] with input as doc
	contains(msg, "fault-injection annotation")
}

test_fault_annotation_denied_on_any_kind {
	# The rule is kind-agnostic: any rendered doc carrying the marker is denied,
	# not only Ingress (a chart template must never embed it anywhere).
	doc := {
		"kind": "Deployment",
		"metadata": {
			"name": "opengate-server",
			"annotations": {"fault.opengate.dev/scenario": "edge-502"},
		},
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
	deny[msg] with input as doc
	contains(msg, "fault-injection annotation")
}
