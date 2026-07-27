#!/usr/bin/env bash

# placeholder for the coming e2e suite; replace with the real tests.
# When run in CI via hack/test-e2e.sh, the project-wrapper injects
# STACKIT_SERVICE_ACCOUNT_KEY and STACKIT_PROJECT_ID into the environment before
# this script runs, pointing at a freshly created, throw-away STACKIT project.

set -o errexit
set -o nounset
set -o pipefail

echo "> Running cloud-e2e suite against STACKIT project ${STACKIT_PROJECT_ID:-<unset>}"
echo "> Versions: k8s=${K8S_VERSION:-<unset>} image=${IMAGE_VERSION:-<unset>}"
echo "TODO: implement the cloud-e2e test suite"
