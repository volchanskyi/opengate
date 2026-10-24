"""Go modules and the standard library: govulncheck over every package the server builds,
tests included, against the database the gauntlet's own scan fetches."""

import os

import fetch
from report import Finding, Reading


def read(root):
    out = fetch.run(["bash", os.path.join(root, "scripts", "govulncheck-scan.sh"), "-scan", "package",
                     "-format", "json"], what="govulncheck")
    messages = fetch.json_stream(out, "govulncheck")
    modules = [message["SBOM"] for message in messages if "SBOM" in message]
    if not modules:
        raise fetch.Unreadable("govulncheck: the report lists no modules")
    count = len(modules[0].get("modules") or []) + 1
    findings = []
    for message in messages:
        finding = message.get("finding")
        if not finding:
            continue
        top = (finding.get("trace") or [{}])[0]
        module, version = top.get("module"), top.get("version")
        if not module or not version:
            raise fetch.Unreadable(f"govulncheck: {finding.get('osv')} names no module and version")
        fix = (finding.get("fixed_version") or "").lstrip("v")
        if module == "stdlib":
            remedy = f"move server/go.mod's toolchain line and every workflow go-version to go{fix}"
        else:
            remedy = f"cd server && go get {module}@v{fix} && go mod tidy"
        findings.append(Finding("go", "server", module, version.lstrip("v"), finding["osv"], fix,
                                remedy=remedy))
    return Reading("Go modules", count, findings)
