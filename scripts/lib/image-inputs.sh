#!/usr/bin/env bash
# The repository paths the server image is built from: the Dockerfile's COPY inputs. Two commits
# that agree on these build the same server.

# image_inputs prints each pathspec on a line of its own.
image_inputs() {
  printf '%s\n' 'server/**' 'web/**' 'Dockerfile'
}
