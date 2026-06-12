#!/usr/bin/env bash
# Configure the Docker Hub mirror or authenticate fallback pulls.

set -euo pipefail

configure_mirror() {
  local attempt

  sudo mkdir -p /etc/docker
  printf '%s\n' '{"registry-mirrors":["https://mirror.gcr.io"]}' \
    | sudo tee /etc/docker/daemon.json >/dev/null
  sudo systemctl restart docker

  for ((attempt = 1; attempt <= 15; attempt++)); do
    if docker info >/dev/null 2>&1; then
      echo "docker up with registry mirror"
      return 0
    fi
    sleep 1
  done

  echo "::error::docker daemon did not come back after configuring the registry mirror"
  return 1
}

login_docker_hub() {
  : "${DH_USER:?DH_USER is required}"
  : "${DH_TOKEN:?DH_TOKEN is required}"

  printf '%s' "$DH_TOKEN" | docker login -u "$DH_USER" --password-stdin
  echo "authenticated to Docker Hub; fallback pulls bypass the anonymous limit"
}

case "${1:-}" in
  configure) configure_mirror ;;
  login) login_docker_hub ;;
  *)
    echo "usage: $0 {configure|login}" >&2
    exit 2
    ;;
esac
