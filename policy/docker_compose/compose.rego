# Compose stacks are throwaway rigs on a test machine. Only the browser stack's two routers hold
# network-admin, which they need to translate, filter and shape the networks behind them.

package main

routers := {"router-office", "router-home"}

capability(raw) := name {
	upper_raw := upper(raw)
	name := trim_prefix(upper_raw, "CAP_")
}

deny[msg] {
	some name
	capability(input.services[name].cap_add[_]) == "NET_ADMIN"
	not routers[name]
	msg := sprintf("services.%v: holds NET_ADMIN — only the stack's two routers may", [name])
}

deny[msg] {
	some name
	capability(input.services[name].cap_add[_]) == "ALL"
	msg := sprintf("services.%v: adds every capability — name the ones it needs", [name])
}

deny[msg] {
	some name
	input.services[name].privileged == true
	msg := sprintf("services.%v: runs privileged — add the capabilities it needs instead", [name])
}

deny[msg] {
	image := input.services[name].image
	endswith(image, ":latest")
	msg := sprintf("services.%v: image %q uses literal :latest — pin a concrete tag", [name, image])
}

# A colon before the last slash is a registry port, so the tag is read after it.
deny[msg] {
	image := input.services[name].image
	parts := split(image, "/")
	not contains(parts[count(parts) - 1], ":")
	msg := sprintf("services.%v: image %q has no tag — append :<version>", [name, image])
}
