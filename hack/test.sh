#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source "$(dirname "$0")/common.sh"

echo "> Unit Tests"

test_flags=
# If running in Prow, we want to generate a machine-readable output file under the location specified via $ARTIFACTS.
# This will add a JUnit view above the build log that shows an overview over successful and failed test cases.
if [ -n "${CI:-}" ] && [ -n "${ARTIFACTS:-}" ]; then
  mkdir -p "$ARTIFACTS/junit"
  trap "collect_reports \"$ARTIFACTS/junit\"" EXIT
  test_flags="--ginkgo.junit-report=junit.xml"
  # Use Ginkgo timeout in Prow to print everything that is buffered in GinkgoWriter.
  test_flags+=" --ginkgo.timeout=2m"
else
  # We don't want Ginkgo's timeout flag locally because it causes skipping the test cache.
  timeout_flag="-timeout=2m"
fi

go test ${timeout_flag:-} "$@" $test_flags
