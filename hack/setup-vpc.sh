#!/usr/bin/env bash
set -eou pipefail

IAAS_API=${IAAS_API:="iaas.api.stackit.cloud"}
REGION=${REGION:="eu01"}
CLUSTER=${CLUSTER:="kubernetes"}

base_url=https://$IAAS_API/v2alpha1/projects/$PROJECT_ID
payload_file=/tmp/payload.json
config_file=dev/config.yaml
response_file=/tmp/response.json

print_fail() {
  echo "iaas call failed, printing response"
  cat $response_file
}

wait_for_network_ready() {
  id=$1
  status=$(stackit -p $PROJECT_ID network describe $id -o json | jq -r .status)
  local max_attempts=10
  local attempts=0
  while [[ $status != "CREATED" ]]; do
    status=$(stackit -p $PROJECT_ID network describe $id -o json | jq -r .status)
    echo "waiting for network $id to get ready, got status $status"
    sleep 1
    attempts=$attempts+1
    if [ $attempts -eq $max_attempts]; then
      echo "max attempts reached for network getting ready, got status $status"
      exit 1
    fi
  done
  echo "network ready"
}

trap print_fail ERR

echo "> checking if vpc exists"
VPC_ID=$(stackit curl -X GET --fail "${base_url}"/vpcs?label_selector=cluster=$CLUSTER | jq -r .items[].id)
if [[ -z $VPC_ID ]]; then
  echo "> vpc missing, creating one"
  cat <<EOF >$payload_file
{
  "labels": {
    "cluster": "$CLUSTER"
  },
  "name": "kubernetes"
}
EOF
  stackit curl -X POST --fail -H "Content-Type: application/json" --data "@$payload_file" "${base_url}"/vpcs --output $response_file
  VPC_ID=$(cat $response_file | jq -r .id)
fi

echo "> enabling vpc for region $REGION"
(
  if ! stackit curl --fail -X GET "$base_url"/vpcs/"${VPC_ID}"/regions/"${REGION}" --output /dev/null; then
    cat <<EOF >$payload_file
{
  "ipv4": {
    "defaultNameservers": ["1.1.1.1"]
  }
}
EOF
    stackit curl --fail -H "Content-Type: application/json" --data "@$payload_file" -X PUT "$base_url"/vpcs/"${VPC_ID}"/regions/"${REGION}" --output /dev/null
  fi
)

echo "> checking if network range exists"
NETWORK_RANGE_ID=$(stackit curl -X GET "$base_url"/vpcs/"${VPC_ID}"/regions/"${REGION}"/network-ranges?label_selector=cluster=$CLUSTER | jq -r .items[].id)
if [[ -z $NETWORK_RANGE_ID ]]; then
  echo "> network range missing, creating one"
  cat <<EOF >$payload_file
{
  "defaultPrefixLen": 25,
  "ipVersion": "ipv4",
  "labels": {
    "cluster": "$CLUSTER"
  },
  "maxPrefixLen": 29,
  "minPrefixLen": 24,
  "prefix": "10.0.0.0/8"
}
EOF
  stackit curl --fail -X POST -H "Content-Type: application/json" --data "@$payload_file" "$base_url"/vpcs/"${VPC_ID}"/regions/"${REGION}"/network-ranges --output $response_file
  NETWORK_RANGE_ID=$(cat $response_file | jq -r .id)
fi

echo "> checking if routing table exists"
RT_ID=$(stackit curl -X GET "$base_url"/vpcs/"${VPC_ID}"/regions/"${REGION}"/routing-tables?label_selector=cluster=kubernetes | jq -r .items[].id)
if [[ -z $RT_ID ]]; then
  cat <<EOF >$payload_file
{
  "labels": {
    "cluster": "$CLUSTER"
  },
  "name": "$CLUSTER"
}
EOF
  stackit curl --fail -X POST -H "Content-Type: application/json" --data "@$payload_file" "$base_url"/vpcs/"${VPC_ID}"/regions/"${REGION}"/routing-tables --output $response_file
  RT_ID=$(cat $response_file | jq -r .id)
fi

echo "> checking if network exists"
NETWORK_ID=$(stackit -p $PROJECT_ID network list --label-selector cluster=$CLUSTER -o json | jq -r .[].id)
if [[ -z $NETWORK_ID ]]; then
  cat <<EOF >$payload_file
{
  "labels": {
    "cluster": "$CLUSTER"
  },
  "ipv4": {
    "prefixLength": 25,
    "vpcNetworkRangeId": "$NETWORK_RANGE_ID"
  },
  "name": "kubernetes",
  "vpcId": "$VPC_ID",
  "routingTableId": "$RT_ID",
  "routed": true
}
EOF
  stackit curl --fail -X POST -H "Content-Type: application/json" --data "@$payload_file" "$base_url"/regions/"$REGION"/networks --output $response_file
  NETWORK_ID=$(cat $response_file | jq -r .id)
fi
wait_for_network_ready $NETWORK_ID
NETWORK_PREFIX=$(stackit -p $PROJECT_ID network describe $NETWORK_ID -o json | jq -r .ipv4.prefixes)

echo "> vpc id: $VPC_ID"
echo "> network range ID: $NETWORK_RANGE_ID"
echo "> routing table id: $RT_ID"
echo "> network id: $NETWORK_ID"
echo "> network ipv4 prefix: $NETWORK_PREFIX"

echo "> generating $config_file for cloud-controller-manager"
cat <<EOF >$config_file
global:
  projectId: $PROJECT_ID
  region: $REGION
  vpcId: $VPC_ID
  apiEndpoints:
    iaasApi: https://$IAAS_API
loadBalancer:
  networkId: $NETWORK_ID
route:
  routingTableId: $RT_ID
EOF
