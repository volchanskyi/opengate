#!/usr/bin/env bash
# Tests that the workflows reach the cluster as their own narrowed login, keep Helm's records in
# configmaps, read neither shared secret, and leave every cluster-wide write to the gated job.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "workflow-identity:"
python3 - "$REPO_ROOT" <<'PY'
import glob
import os
import re
import sys

import yaml

root = sys.argv[1]
passed = failed = 0


def check(label, ok, detail=""):
    global passed, failed
    if ok:
        passed += 1
        print(f"  ok   {label}")
    else:
        failed += 1
        print(f"  FAIL {label}{': ' + detail if detail else ''}", file=sys.stderr)


def joined(text):
    return text.replace("\\\n", " ")


workflows = {}
for path in sorted(glob.glob(os.path.join(root, ".github/workflows/*.yml"))):
    text = open(path, encoding="utf-8").read()
    workflows[os.path.basename(path)] = (text, yaml.safe_load(text))
check("the sweep reads the workflows", len(workflows) > 10, f"{len(workflows)} read")

# Scripts a workflow runs that call Helm against the cluster.
HELM_SCRIPTS = ("scripts/fault/bad-rollout.sh", "deploy/scripts/ingress-controller-apply.sh",
                "deploy/scripts/cluster-apply.sh")
helm_users = []
for name, (text, doc) in workflows.items():
    body = joined(text)
    if re.search(r"\bhelm (upgrade|install|rollback|history|list|status|get|uninstall)\b", body) or any(
            s in body for s in HELM_SCRIPTS):
        helm_users.append(name)
        env = (doc.get("env") or {})
        check(f"{name} keeps Helm's release records in configmaps", env.get("HELM_DRIVER") == "configmap",
              f"workflow env HELM_DRIVER={env.get('HELM_DRIVER')!r}")
check("the sweep finds the workflows that run Helm", {"cd.yml", "fault-tolerance.yml",
                                                     "terraform-drift.yml"} <= set(helm_users), str(helm_users))

reads = re.compile(r"get\s+secrets?\s+(?:opengate-secrets|opengate-authority)\b|secret/opengate-(?:secrets|authority)\b")
for name, (text, _) in workflows.items():
    hits = [m.group(0) for m in reads.finditer(joined(text)) if "can-i" not in m.string[max(0, m.start() - 40):m.start()]]
    check(f"{name} reads neither shared secret", not hits, ", ".join(hits))

logins = 0
for name, (text, doc) in workflows.items():
    for job_name, job in (doc.get("jobs") or {}).items():
        for step in job.get("steps") or []:
            if "oci-kube-setup" not in str(step.get("uses", "")):
                continue
            logins += 1
            user = (step.get("with") or {}).get("oci-user", "")
            if "OCI_ADMIN_" in user:
                check(f"{name} {job_name}: the administrator login sits behind production's approval",
                      job.get("environment") == "production" and "cluster-apply.sh" in yaml.safe_dump(job))
            else:
                key = (step.get("with") or {}).get("oci-private-key", "")
                check(f"{name} {job_name}: signs in as the workflows' own login",
                      "OCI_WORKFLOW_USER_OCID" in user and "OCI_WORKFLOW_PRIVATE_KEY" in key, user)
check("the sweep finds every cluster sign-in", logins >= 12, f"{logins} found")
check("no workflow names the retired repository login",
      not any(re.search(r"secrets\.OCI_(USER_OCID|FINGERPRINT|PRIVATE_KEY)\b", t) for t, _ in workflows.values()))

# A called workflow refuses to start without each secret it declares required, so its declared
# secrets are exactly the ones it reads.
called = 0
for name, (text, doc) in workflows.items():
    trigger = doc.get("on", doc.get(True)) or {}
    if not isinstance(trigger, dict) or "workflow_call" not in trigger:
        continue
    called += 1
    declared = set(((trigger["workflow_call"] or {}).get("secrets") or {}).keys())
    read = set(re.findall(r"secrets\.([A-Z0-9_]+)", text)) - {"GITHUB_TOKEN"}
    check(f"{name} declares exactly the secrets it reads", declared == read,
          f"declared and unread {sorted(declared - read)}, read and undeclared {sorted(read - declared)}")
check("the sweep finds the called workflows", called >= 1, f"{called} found")

cd_text, cd = workflows["cd.yml"]
jobs = cd["jobs"]
cluster = [n for n, j in jobs.items() if "cluster-apply.sh" in yaml.safe_dump(j)]
check("one cd job applies the cluster-wide pieces", len(cluster) == 1, str(cluster))
if cluster:
    gate = cluster[0]
    check("the cluster job waits for production's approval", jobs[gate].get("environment") == "production")
    staging_needs = jobs["deploy-staging-k8s"].get("needs", [])
    check("staging waits for the cluster job", gate in (staging_needs if isinstance(staging_needs, list) else [staging_needs]))
for job_name in ("deploy-staging-k8s", "deploy-production-k8s"):
    body = joined(yaml.safe_dump(jobs[job_name], width=10000))
    check(f"{job_name} reads back what its login may do", "deploy/scripts/workflow-access-readback.sh" in body)
    check(f"{job_name} creates no namespace", "create namespace" not in body)
    check(f"{job_name} installs no ingress controller", "ingress-controller-apply.sh apply" not in body)
    check(f"{job_name} renders no issuer", "--set certManager.create=false" in body)
prod = joined(yaml.safe_dump(jobs["deploy-production-k8s"], width=10000))
check("production proves the master key's guard rule", "deploy/scripts/authority-readers-check.sh" in prod)
check("the routine monitoring release renders no cluster roles", "--set clusterRBAC.create=false" in prod)

print(f"\nworkflow-identity: {passed} passed, {failed} failed")
sys.exit(1 if failed else 0)
PY
