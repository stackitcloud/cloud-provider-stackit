#!/usr/bin/env bash

# placeholder for the coming e2e suite; replace with the real tests.
# When run in CI via hack/test-e2e.sh, the project-wrapper injects
# STACKIT_SERVICE_ACCOUNT_KEY and STACKIT_PROJECT_ID into the environment before
# this script runs, pointing at a freshly created, throw-away STACKIT project.

set -o errexit
set -o nounset
set -o pipefail

echo "> Running cloud-e2e suite against a STACKIT project"
echo "> STACKIT_PROJECT_ID=${STACKIT_PROJECT_ID:-<unset>}"
echo "> K8S_VERSION=${K8S_VERSION:-<unset>}"
echo "> IMAGE_VERSION=${IMAGE_VERSION:-<unset>}"
echo "> IMAGE_NAME=${IMAGE_NAME:-<unset>}"
echo "> MACHINE_TYPE=${MACHINE_TYPE:-<unset>}"
echo "TODO: implement the cloud-e2e test suite"
