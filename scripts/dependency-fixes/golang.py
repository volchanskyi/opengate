"""Go modules and the standard library: govulncheck over every package the server builds,
tests included, against the database the gauntlet's own scan fetches."""

import os

import fetch
import osv
import severity
from report import NEWER_LINE, Finding, Reading


def shipped_modules(root):
    """Every module linked into the server binary, the standard library named stdlib."""
    out = fetch.run(["go", "list", "-deps", "-f", "{{if .Module}}{{.Module.Path}}{{else}}stdlib{{end}}",
                     "./cmd/meshserver"], cwd=os.path.join(root, "server"), what="go list the server build")
    modules = {line.strip() for line in out.splitlines() if line.strip()}
    if not modules:
        raise fetch.Unreadable("go list: the server build names no module")
    return modules


def mark_serious(root, findings):
    """Reads each newer-line finding's record for its aliases, and marks the serious ones."""
    newer = [finding for finding in findings if finding.answer == NEWER_LINE]
    if not newer:
        return
    records = {}
    for finding in newer:
        records[finding.advisory] = osv.alias_record(finding.advisory) or {"id": finding.advisory}
        finding.aliases = osv.aliases(records[finding.advisory])
    severity.mark(newer, shipped_modules(root), lambda finding: severity.of(records[finding.advisory]))


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
    mark_serious(root, findings)
    return Reading("Go modules", count, findings)
