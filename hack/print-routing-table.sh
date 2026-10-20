#!/usr/bin/env bash
set -euo pipefail

# TODO: This is a temporary script to ease the setup for VPCs as long as the stackit CLI does not support them.
# Refactor/delete once we use the CLI.

PROJECT_ID=$1
VPC_ID=${2:-"$(cat dev/config.yaml | yq .global.vpcId)"}
RT_ID=${3:-"$(cat dev/config.yaml | yq .route.routingTableId)"}
CLUSTER=${CLUSTER:="kubernetes"}
base_url=https://$IAAS_API/v2alpha1/projects/$PROJECT_ID

if [[ -z $PROJECT_ID ]]; then
  echo "must provide project ID as arg 1"
  exit 1
fi

IAAS_API=${IAAS_API:="iaas.api.stackit.cloud"}
REGION=${REGION:="eu01"}
CLUSTER=${CLUSTER:="kubernetes"}

url="$base_url/vpcs/${VPC_ID}/regions/${REGION}/routing-tables/$RT_ID/static-routes?label_selector=kubernetes.io_cluster=$CLUSTER"
echo "issuing stackit curl $url"
stackit curl --fail -X GET "$url" | yq -p=json
