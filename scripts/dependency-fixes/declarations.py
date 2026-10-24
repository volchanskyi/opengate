"""Nothing escapes: every tracked declaration of a dependency is one a reader reads.

A declaration is a dependency file, a `uses:` line, or an install command naming a package.
Images are swept by the image reader itself, and every Cargo.toml by the Rust reader's walk of
the workspaces. A file that only looks like a declaration is listed in scope.json with a reason.
"""

import fnmatch
import os
import re

import images
import repo
import tools

UNREAD = ("yarn.lock", "pnpm-lock.yaml", "requirements.txt", "Pipfile", "Pipfile.lock", "poetry.lock",
          "pyproject.toml", "setup.py", "Gemfile", "Gemfile.lock", "composer.json", "Chart.lock", "bun.lockb")
_USES = re.compile(r"^\s*-?\s*uses:\s*['\"]?([^'\"\s#]+)", re.M)
_INSTALL = re.compile(r"(?:^|[\s;&|(])(go install|cargo install|pipx? install|npm (?:install|i) (?:-g|--global))"
                      r" +(\S[^\n;&|#]*)")
_EXPRESSION = re.compile(r"\$\{\{[^}]*\}\}")
_VALUED = ("--version", "--git", "--rev", "--branch", "--tag", "--root", "--index-url", "--target", "-r")


def _covered(root):
    """For each install verb, the package names a manifest row's source covers."""
    covered = {"go": set(), "cargo": set(), "pip": set(), "npm": set()}
    verbs = {"Go": "go", "crates.io": "cargo", "PyPI": "pip", "npm": "npm", "git-rev": "cargo"}
    for _, _, source in tools.rows(root):
        ecosystem, _, spec = source.partition(":")
        if ecosystem in verbs:
            name = spec.split("@")[0] if ecosystem == "git-rev" else spec.split(":")[0]
            covered[verbs[ecosystem]].add(name.rsplit("/", 1)[-1] if ecosystem == "git-rev" else name.lower())
    return covered


def _named(words):
    """The packages an install's words name, its flags and their values left out."""
    named, skip = [], False
    for word in (w.strip("'\"") for w in words):
        if skip or word.startswith("-"):
            skip = not skip and word in _VALUED
            continue
        if re.match(r"^[@A-Za-z][\w./@=$-]*$", word) and "$" not in re.split(r"(?<=.)[@=]", word)[0]:
            named.append(word)
    return named


def _uncovered(verb, package, covered):
    kind = verb.split()[0][:3] if verb.startswith("pip") else verb.split()[0]
    if kind == "go":
        module = package.split("@")[0].lower()
        return not any(module == m or module.startswith(m + "/") for m in covered["go"])
    if kind == "npm":
        return package.rsplit("@", 1)[0].lower() not in covered["npm"]
    return re.split(r"[@=<>~]", package)[0].lower() not in covered[kind]


def _file_findings(path, tracked):
    name, folder = os.path.basename(path), os.path.dirname(path)
    if name in UNREAD or (name == "go.mod" and path != "server/go.mod"):
        return [f"{path}: a dependency file no reader reads"]
    if name == "package.json" and os.path.join(folder, "package-lock.json") not in tracked:
        return [f"{path}: an npm manifest with no lockfile beside it"]
    return []


def _text_findings(path, text, covered):
    found = []
    if path.endswith((".yml", ".yaml")) and not path.startswith((".github/workflows/", ".github/actions/")):
        found += [f"{path}: \"uses: {uses}\" outside the workflows" for uses in _USES.findall(text)]
    joined = _EXPRESSION.sub("$expression", re.sub(r"\\\n\s*", " ", text))
    for verb, rest in _INSTALL.findall(joined):
        found += [f"{path}: \"{verb} {package}\" installs a package no manifest row names"
                  for package in _named(rest.split())
                  if not package.startswith(".") and _uncovered(verb, package, covered)]
    return found


def unread(root):
    """Every declaration no reader reads, each with where it is written."""
    skipped = [entry["path"] for entry in images.scope().get("files", [])]
    covered, found = _covered(root), []
    tracked = repo.tracked(root)
    for path in tracked:
        if any(fnmatch.fnmatch(path, pattern) for pattern in skipped):
            continue
        found += _file_findings(path, tracked)
        try:
            text = repo.text(root, path)
        except OSError:
            continue
        if "\0" not in text:
            found += _text_findings(path, text, covered)
    return found
