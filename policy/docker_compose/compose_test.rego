package main

test_the_stack_shape_passes {
	count(deny) == 0 with input as {"services": {
		"postgres": {"image": "postgres:17-alpine"},
		"server": {"build": {"context": ".."}},
		"router-office": {"build": {"context": "router"}, "cap_drop": ["ALL"], "cap_add": ["NET_ADMIN", "NET_RAW"]},
		"router-home": {"build": {"context": "router"}, "cap_drop": ["ALL"], "cap_add": ["NET_ADMIN", "NET_RAW"]},
	}}
}

test_network_admin_outside_the_routers_is_denied {
	deny[msg] with input as {"services": {"agent-a": {"build": {"context": ".."}, "cap_add": ["NET_ADMIN"]}}}
	contains(msg, "services.agent-a: holds NET_ADMIN")
}

test_network_admin_with_its_prefix_is_denied {
	deny[msg] with input as {"services": {"browser": {"cap_add": ["cap_net_admin"]}}}
	contains(msg, "services.browser: holds NET_ADMIN")
}

test_every_capability_is_denied_even_to_a_router {
	deny[msg] with input as {"services": {"router-home": {"cap_add": ["ALL"]}}}
	contains(msg, "services.router-home: adds every capability")
}

test_a_privileged_service_is_denied {
	deny[msg] with input as {"services": {"router-office": {"privileged": true}}}
	contains(msg, "services.router-office: runs privileged")
}

test_literal_latest_is_denied {
	deny[msg] with input as {"services": {"x": {"image": "postgres:latest"}}}
	contains(msg, "uses literal :latest")
}

test_an_image_with_no_tag_is_denied {
	deny[msg] with input as {"services": {"x": {"image": "redis"}}}
	contains(msg, "has no tag")
}

test_a_registry_port_is_not_a_tag {
	deny[msg] with input as {"services": {"x": {"image": "registry.test:5000/redis"}}}
	contains(msg, "has no tag")
}
