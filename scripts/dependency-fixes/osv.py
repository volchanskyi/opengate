"""The advisory database the check asks about every ecosystem: OSV, through its batch query for
which advisories touch a release and its record of each advisory's ranges."""

import cache
import fetch
import versions

QUERY = "https://api.osv.dev/v1/querybatch"
RECORD = "https://api.osv.dev/v1/vulns/{}"
BATCH = 500


def _normal(ecosystem, name):
    if ecosystem == "PyPI":
        return name.lower().replace("_", "-").replace(".", "-")
    return name


def _query(query):
    """Every advisory OSV names for QUERY, following its page tokens."""
    found, page = [], None
    while True:
        body = dict(query, page_token=page) if page else query
        result = fetch.http_post_json(QUERY, {"queries": [body]})["results"][0]
        found += result.get("vulns") or []
        page = result.get("next_page_token")
        if not page:
            return found


def matches(queries):
    """For each query, the advisories OSV names: a list of {id, modified} per query."""
    answers = []
    for start in range(0, len(queries), BATCH):
        chunk = queries[start:start + BATCH]
        reply = fetch.http_post_json(QUERY, {"queries": chunk})
        results = reply.get("results")
        if not isinstance(results, list) or len(results) != len(chunk):
            raise fetch.Unreadable(f"POST {QUERY}: {len(chunk)} queries, an answer of another shape")
        for query, result in zip(chunk, results):
            if result.get("next_page_token"):
                answers.append(_query(query))
            else:
                answers.append(result.get("vulns") or [])
    return answers


def record(advisory):
    """The advisory's full record, cached by its identifier and modification time."""
    key = f"{advisory['id']}@{advisory.get('modified', '')}"
    if not advisory.get("modified"):
        return fetch.http_json(RECORD.format(advisory["id"]))
    return cache.remember("osv", key, lambda: fetch.http_json(RECORD.format(advisory["id"])))


def package_query(ecosystem, name, version):
    return {"package": {"ecosystem": ecosystem, "name": name}, "version": str(version).lstrip("vV")
            if ecosystem in ("Go", "GitHub Actions") else str(version)}


def _entries(ecosystem, name, advisory):
    """The record's affected entries for NAME in ECOSYSTEM."""
    wanted = _normal(ecosystem, name)
    return [affected for affected in advisory.get("affected") or []
            if (affected.get("package") or {}).get("ecosystem", "").split(":")[0] == ecosystem
            and _normal(ecosystem, (affected.get("package") or {}).get("name", "")) == wanted]


def fixes(ecosystem, name, version, advisory):
    """The release that fixes NAME at VERSION for one advisory record: a version, '' for none,
    or None when the record does not touch that version."""
    entries = _entries(ecosystem, name, advisory)
    ranges = [rng for affected in entries for rng in affected.get("ranges") or []]
    plain = str(version).lstrip("vV")
    listed = any(plain == v.lstrip("vV") for affected in entries for v in affected.get("versions") or [])
    plain = plain if ecosystem in ("Go", "GitHub Actions") else str(version)
    try:
        fix = versions.fix_for(plain, ranges)
    except ValueError as error:
        raise fetch.Unreadable(f"{advisory.get('id')}: a range of {name} does not read: {error}") from error
    if fix is None:
        return "" if listed else None
    return fix


def aliases(advisory):
    return tuple(sorted(set(advisory.get("aliases") or [])))


def summary(advisory):
    text = (advisory.get("summary") or advisory.get("details") or "").strip()
    return text.splitlines()[0][:120] if text else ""


def check_packages(packages):
    """Ask about many (ecosystem, name, version) at once; yields (package, record, fix) for each
    advisory that touches the version, fix being a version or ''."""
    queries = [package_query(*package) for package in packages]
    for package, found in zip(packages, matches(queries)):
        for advisory in found:
            full = record(advisory)
            fix = fixes(package[0], package[1], package[2], full)
            if fix is not None:
                yield package, full, fix


def affects(ecosystem, name, version, advisory):
    """Whether ADVISORY's record touches NAME at VERSION."""
    return version is not None and fixes(ecosystem, name, version, advisory) is not None
