#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

IMAGE=$1

if [[ -z ${IMAGE} ]]; then
  echo must set image as arg 1
  exit 1
fi

if [[ -z ${APKO_EXTRA_PACKAGES} ]]; then
  APKO_EXTRA_PACKAGES=""
fi

if [[ -z ${LOCAL} ]]; then
  LOCAL="false"
fi

function comma_seperated() {
  local arr=("$@")
  local IFS=,
  echo "${arr[*]}"
}

# when running in prow, the PR is merged into the base branch resulting in git branch --show-current reporting the base branch.
# To tag PRs with their branch in prow, we need to use the env variables injected into the test pod
if [[ -z "${PULL_BASE_REF:=""}" ]]; then
  PULL_BASE_REF="$(git branch --show-current)"
fi

# tag for dev builds
# generate a tag that renovate can easily compare (similar to prow's image tags)
# format: v<commit-timestamp>-<short-commit-sha>
tags=(
  "v$(date -d @$(git show -s --format=%ct @) -u +%Y%m%d%H%M%S)-$(git rev-parse HEAD | head -c7)"
  ${VERSION}
  $(echo ${PULL_BASE_REF} | sed 's/\//_/g' | sed 's/#//g')
)

if [[ -n "${PULL_NUMBER:=""}" ]]; then
  tags+=("pr-${PULL_NUMBER}")
fi

# tag for release builds
if git_tag="$(git describe --tags --exact-match 2>/dev/null)"; then
  tags+=("$git_tag")
fi

REPO=${REGISTRY}/stackitcloud/${IMAGE}
BASE_IMAGE=${REPO}-base
platforms=(${PLATFORMS})
if [[ -f cmd/$IMAGE/apko-base-image.yaml ]]; then
  if [[ ${LOCAL} == "true" ]]; then
    BASE_IMAGE=ko.local/${IMAGE}-base
    platforms=($(uname -m))
  fi

  APKO_IMAGE=$(apko publish --local=${LOCAL} cmd/${IMAGE}/apko-base-image.yaml ${BASE_IMAGE} --sbom=false -p "${APKO_EXTRA_PACKAGES}" --arch $(comma_seperated "${platforms[@]}"))
  export KO_DEFAULTBASEIMAGE=${APKO_IMAGE}
  if [[ ${LOCAL} == "true" ]]; then
    # apko only published images to registry apko.local with repo cache when running in local mode.
    # Need to tag to registry ko.local for ko to see that the image needs to be resolved locally
    # will be fixed in https://github.com/chainguard-dev/apko/pull/1781
    docker tag ${APKO_IMAGE} ${BASE_IMAGE}
    export KO_DEFAULTBASEIMAGE=${BASE_IMAGE}
  fi
fi

# convert platforms to include linux as os
ko_platforms=()
for p in "${platforms[@]}"; do
  ko_platforms+=("linux/${p}")
done

labels=(
  org.opencontainers.image.created="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  org.opencontainers.image.revision=${VERSION}
  org.opencontainers.image.source=https://github.com/stackitcloud/cloud-provider-stackit
  org.opencontainers.image.url=https://github.com/stackitcloud/cloud-provider-stackit
)

KO_DOCKER_REPO=${REPO} \
  ko build --local=${LOCAL} \
  -t $(comma_seperated "${tags[@]}") \
  --sbom=none \
  --platform $(comma_seperated "${ko_platforms[@]}") \
  --bare \
  --image-label=$(comma_seperated "${labels[@]}") \
  ./cmd/$IMAGE
