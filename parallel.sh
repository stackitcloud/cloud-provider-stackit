#!/usr/bin/env bash

set -euo pipefail

export STACKIT_SERVICE_ACCOUNT="$(cat ./credentials.json)"

run_id="stackit-parallel"
cluster_name="kt2-par"
kubernetes_version="1.35.6"
rundir_root="${PWD}/_rundir"
artifacts_root="${PWD}/_artifacts-parallel"
testdriver="${rundir_root}/${run_id}/csi-testdriver.yaml"
parallel_nodes="$(getconf _NPROCESSORS_ONLN 2>/dev/null || true)"

if [[ ! "${parallel_nodes}" =~ ^[0-9]+$ ]] || (( parallel_nodes < 1 )); then
  parallel_nodes=4
fi

go run ./cmd/kubetest2-stackit \
  --run-id "${run_id}" \
  --cluster-name "${cluster_name}" \
  --rundir "${rundir_root}" \
  --artifacts "${artifacts_root}" \
  --up \
  --test=ginkgo \
  --project-id e928aade-ce15-4188-8230-c162c8fb3bd4 \
  --region eu01 \
  --kubernetes-version "${kubernetes_version}" \
  --availability-zone eu01-1 \
  --machine-type c2i.2 \
  --node-image-name flatcar \
  --node-image-version 4593.2.3-containerd2.1.9 \
  --csi-image-name ttl.sh/csi-plugin/stackit-csi-plugin \
  --csi-image-tag 5h \
  -- \
  --test-package-version="v${kubernetes_version}" \
  --focus-regex="External.Storage" \
  --skip-regex="\[Feature:|\[Disruptive\]|\[Serial\]" \
  --ginkgo-args="-v" \
  --parallel="${parallel_nodes}" \
  --test-args="--storage.testdriver=${testdriver}"
