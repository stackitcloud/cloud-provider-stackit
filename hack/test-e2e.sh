#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

echo "> E2E Tests"

# The actual e2e suite is a self-contained shell script. It is told which STACKIT
# project to test against via the STACKIT_SERVICE_ACCOUNT_KEY and STACKIT_PROJECT_ID
# environment variables.
e2e_suite="$(dirname "$0")/../test/e2e/run.sh"

# When running in CI, the project-wrapper creates a throw-away STACKIT portal project,
# runs the suite against it, and deletes the project again afterwards. Locally we run
# the suite directly against the developer's own credentials.
if [ -n "${CI:-}" ]; then
  go run "$(dirname "$0")/../test/project-wrapper" "$e2e_suite" "$@"
else
  "$e2e_suite" "$@"
fi
