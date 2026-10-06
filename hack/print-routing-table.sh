#!/usr/bin/env bash
set -eo pipefail

PROJECT_ID=$1
VPC_ID=$2
RT_ID=$3
CLUSTER=${CLUSTER:="kubernetes"}
base_url=https://$IAAS_API/v2alpha1/projects/$PROJECT_ID

if [[ -z $PROJECT_ID ]]; then
  echo "must provide project ID as arg 1"
  exit 1
fi

if [[ -z $VPC_ID ]]; then
  VPC_ID=$(cat dev/config.yaml | yq .global.vpcId)
fi

if [[ -z $RT_ID ]]; then
  RT_ID=$(cat dev/config.yaml | yq .route.routingTableId)
fi

IAAS_API=${IAAS_API:="iaas.api.stackit.cloud"}
REGION=${REGION:="eu01"}
CLUSTER=${CLUSTER:="kubernetes"}

url="$base_url/vpcs/${VPC_ID}/regions/${REGION}/routing-tables/$RT_ID/static-routes?label_selector=kubernetes.io_cluster=$CLUSTER"
echo "issuing stackit curl $url"
stackit curl --fail -X GET "$url" | yq -p=json
