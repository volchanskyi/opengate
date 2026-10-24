"""Container registries read anonymously: an image reference's parts, the digest a tag points
at, and the tags a repository publishes."""

import dataclasses
import functools
import re
import urllib.parse

import fetch

MANIFESTS = ("application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json,"
             " application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json")
_HOST = re.compile(r"^[a-z0-9-]+(\.[a-z0-9-]+)+(:\d+)?$")
_NEXT = re.compile(r'<([^>]+)>\s*;\s*rel="?next"?')


@dataclasses.dataclass(frozen=True)
class Image:
    registry: str
    repository: str
    tag: str
    digest: str = ""

    @property
    def name(self):
        return self.repository if self.registry == "docker.io" else f"{self.registry}/{self.repository}"

    def __str__(self):
        return f"{self.name}:{self.tag}" + (f"@{self.digest}" if self.digest else "")

    def at(self, tag):
        return Image(self.registry, self.repository, tag)


def parse(reference):
    """The Image REFERENCE names, its registry defaulting to Docker Hub's library."""
    name, _, digest = reference.partition("@")
    first, _, rest = name.partition("/")
    if rest and _HOST.match(first):
        registry, path = first, rest
    else:
        registry, path = "docker.io", name
    path, _, tag = path.rpartition(":") if ":" in path else (path, "", "latest")
    if registry == "docker.io" and "/" not in path:
        path = f"library/{path}"
    return Image(registry, path, tag, digest)


def _endpoint(registry):
    return "registry-1.docker.io" if registry == "docker.io" else registry


@functools.lru_cache(maxsize=None)
def _token(registry, repository):
    if registry == "docker.io":
        url = f"https://auth.docker.io/token?service=registry.docker.io&scope=repository:{repository}:pull"
    elif registry == "ghcr.io":
        url = f"https://ghcr.io/token?scope=repository:{repository}:pull"
    else:
        return ""
    return fetch.http_json(url).get("token", "")


def _auth(image):
    token = _token(image.registry, image.repository)
    return [f"Authorization: Bearer {token}"] if token else []


def digest(image):
    """The digest IMAGE's tag points at now, or the digest it is pinned to."""
    if image.digest:
        return image.digest
    url = f"https://{_endpoint(image.registry)}/v2/{image.repository}/manifests/{image.tag}"
    headers = fetch.http_headers(url, [f"Accept: {MANIFESTS}"] + _auth(image))
    found = headers.get("docker-content-digest", "")
    if not found.startswith("sha256:"):
        raise fetch.Unreadable(f"{image}: the registry named no digest for the tag")
    return found


@functools.lru_cache(maxsize=None)
def tags(image):
    """Every tag of IMAGE's repository, read on each page the registry's Link header names."""
    endpoint = _endpoint(image.registry)
    url, listed, seen = f"https://{endpoint}/v2/{image.repository}/tags/list?n=10000", [], set()
    while url:
        if url in seen:
            raise fetch.Unreadable(f"{image.name}: the tag listing names a page it already gave")
        seen.add(url)
        body, headers = fetch.http_page(url, _auth(image))
        listed += fetch.parse_json(body, f"GET {url}").get("tags") or []
        following = _NEXT.search(headers.get("link", ""))
        url = urllib.parse.urljoin(f"https://{endpoint}/", following.group(1)) if following else ""
    return tuple(listed)
