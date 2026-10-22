#!/usr/bin/env bash
# Tests that the workflows' own cluster login may make every call the workflows make, and is refused
# a secret listing, any read of the master key's secret, a shell in the server, and cluster writes.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ACCESS="$REPO_ROOT/deploy/cluster/workflow-access.yaml"

[ -f "$ACCESS" ] || {
  echo "FAIL: $ACCESS is missing" >&2
  exit 1
}

echo "workflow-access:"
python3 - "$ACCESS" <<'PY'
import sys
import yaml

USER = "ocid1.user.oc1..workflow"
docs = [d for d in yaml.safe_load_all(open(sys.argv[1]).read().replace("WORKFLOW_USER_OCID", USER)) if d]

roles = {}
for d in docs:
    if d["kind"] in ("Role", "ClusterRole"):
        roles[(d["kind"], d["metadata"].get("namespace"), d["metadata"]["name"])] = d.get("rules", [])

granted = []
for d in docs:
    if d["kind"] not in ("RoleBinding", "ClusterRoleBinding"):
        continue
    if not any(s.get("kind") == "User" and s.get("name") == USER for s in d.get("subjects", [])):
        continue
    ref = d["roleRef"]
    scope = d["metadata"].get("namespace") if d["kind"] == "RoleBinding" else "*"
    key = (ref["kind"], scope if ref["kind"] == "Role" else None, ref["name"])
    granted.append((scope, roles.get(key, [])))


def allowed(ns, verb, group, resource, name=None):
    for scope, rules in granted:
        if scope not in ("*", ns):
            continue
        for r in rules:
            if group not in r.get("apiGroups", []) or resource not in r.get("resources", []):
                continue
            if verb not in r.get("verbs", []):
                continue
            names = r.get("resourceNames")
            if names and name not in names:
                continue
            return True
    return False


passed = failed = 0


def check(label, want, got):
    global passed, failed
    if want == got:
        passed += 1
        print(f"  ok   {label}")
    else:
        failed += 1
        print(f"  FAIL {label}", file=sys.stderr)


WRITE = ["get", "list", "watch", "create", "update", "patch", "delete"]
RELEASE = [("", "configmaps"), ("", "services"), ("", "serviceaccounts"), ("apps", "deployments"),
           ("apps", "statefulsets"), ("apps", "replicasets"), ("batch", "cronjobs"), ("batch", "jobs"),
           ("networking.k8s.io", "ingresses")]

need = []
for ns in ("opengate", "opengate-staging"):
    need += [(ns, v, g, r, None) for g, r in RELEASE for v in WRITE]
    need += [(ns, v, "", "pods", None) for v in ("get", "list", "watch", "create")]
    need += [(ns, v, "", "pods/portforward", None) for v in ("create", "get")]
    need += [(ns, "create", "", "secrets", None), (ns, "patch", "", "secrets", "opengate-secrets")]
need += [("opengate", v, "", "pods/exec", "opengate-postgres-0") for v in ("create", "get")]
staging_secrets = ["opengate-staging-loadtest-account", "opengate-e2e-enrolment", "opengate-netdrill-enrolment"]
need += [("opengate-staging", v, "", "secrets", s) for s in staging_secrets for v in ("get", "update", "patch", "delete")]
need += [("opengate-staging", v, "", sub, "any-pod") for sub in ("pods/exec", "pods/attach") for v in ("create", "get")]
need += [("opengate-staging", "delete", "", "pods", None), ("opengate-staging", "get", "", "pods/log", None),
         ("opengate-staging", "list", "", "events", None)]
need += [("opengate-staging", v, "coordination.k8s.io", "leases", "opengate-staging-guard") for v in ("get", "update", "delete")]
need += [("opengate-staging", "create", "coordination.k8s.io", "leases", None)]
need += [("opengate-staging", v, "apps", "deployments/scale", None) for v in ("get", "patch", "update")]
monitoring = [("", "configmaps"), ("", "services"), ("", "serviceaccounts"), ("apps", "deployments"),
              ("apps", "statefulsets"), ("apps", "daemonsets"), ("apps", "replicasets"), ("apps", "controllerrevisions")]
need += [("monitoring", v, g, r, None) for g, r in monitoring for v in WRITE]
need += [("monitoring", v, "", "pods", None) for v in ("get", "list", "watch", "create", "delete")]
need += [("monitoring", v, "", sub, "any-pod") for sub in ("pods/exec", "pods/attach") for v in ("create", "get")]
need += [("monitoring", "get", "", "pods/log", None), ("monitoring", "get", "", "services/proxy", None)]
need += [("ingress-nginx", "get", "", "services", None), ("ingress-nginx", "get", "", "configmaps", None),
         ("ingress-nginx", "list", "", "configmaps", None)]
need += [(None, v, "", "nodes", None) for v in ("get", "list")]
need += [(None, "get", "admissionregistration.k8s.io", r, "opengate-authority-readers")
         for r in ("validatingadmissionpolicies", "validatingadmissionpolicybindings")]

missing = [n for n in need if not allowed(*n)]
check(f"the login may make each of the {len(need)} calls the workflows make", [], missing)
for n in missing:
    print(f"       refused: {n}", file=sys.stderr)

for ns in ("opengate", "opengate-staging", "monitoring", "ingress-nginx", "kube-system", None):
    for verb in ("list", "watch"):
        check(f"{verb} on secrets is refused in {ns or 'the cluster'}", False, allowed(ns, verb, "", "secrets"))
# Kubernetes cannot hold create to one name, and a create cannot touch a secret that already exists.
for verb in (v for v in WRITE if v != "create"):
    check(f"{verb} on the master key's secret is refused", False,
          allowed("opengate", verb, "", "secrets", "opengate-authority"))
check("production's shared secret stays unread", False, allowed("opengate", "get", "", "secrets", "opengate-secrets"))
for verb in ("create", "get"):
    check(f"a shell in the production server is refused ({verb})", False,
          allowed("opengate", verb, "", "pods/exec", "opengate-server-6d9f-abcde"))
    check(f"attaching to a production pod is refused ({verb})", False,
          allowed("opengate", verb, "", "pods/attach", "opengate-server-6d9f-abcde"))
cluster = [("", "namespaces"), ("rbac.authorization.k8s.io", "clusterroles"),
           ("rbac.authorization.k8s.io", "clusterrolebindings"),
           ("admissionregistration.k8s.io", "validatingadmissionpolicies"),
           ("admissionregistration.k8s.io", "validatingadmissionpolicybindings"), ("cert-manager.io", "clusterissuers")]
for g, r in cluster:
    for verb in ("create", "update", "patch", "delete"):
        check(f"{verb} on {r} is refused", False, allowed(None, verb, g, r, "opengate-authority-readers"))
for ns in ("opengate", "opengate-staging", "monitoring"):
    for r in ("roles", "rolebindings"):
        check(f"creating {r} in {ns} is refused", False, allowed(ns, "create", "rbac.authorization.k8s.io", r))
check("nothing runs in kube-system", False, allowed("kube-system", "create", "", "pods"))

named = [n for d in docs if d["kind"] in ("Role", "ClusterRole") for rule in d.get("rules", [])
         for n in rule.get("resourceNames", [])]
check("no rule names the master key's secret", False, "opengate-authority" in named)

print(f"\nworkflow-access: {passed} passed, {failed} failed")
sys.exit(1 if failed else 0)
PY
