# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

################################################################################
##                               BUILD ARGS                                   ##
################################################################################
# This build arg allows the specification of a custom Golang image.
ARG GOLANG_IMAGE=golang:1.24.4

# The distroless image on which the CPI manager image is built.
#
# Please do not use "latest". Explicit tags should be used to provide
# deterministic builds. Follow what kubernetes uses to build
# kube-controller-manager, for example for 1.27.x:
# https://github.com/kubernetes/kubernetes/blob/release-1.27/build/common.sh#L99
ARG DISTROLESS_IMAGE=registry.k8s.io/build-image/go-runner:v2.4.0-go1.24.4-bookworm.0

# We use Alpine as the source for default CA certificates and some output
# images
ARG ALPINE_IMAGE=alpine:3.21.2

# stackit-csi-plugin uses Debian as a base image
ARG DEBIAN_IMAGE=registry.k8s.io/build-image/debian-base:bookworm-v1.0.4

################################################################################
##                              BUILD STAGE                                   ##
################################################################################

# Build an image containing a common ca-certificates used by all target images
# regardless of how they are built. We arbitrarily take ca-certificates from
# the amd64 Alpine image.
FROM --platform=${BUILDPLATFORM} ${ALPINE_IMAGE} AS certs
RUN apk add --no-cache ca-certificates


# Build all command targets. We build all command targets in a single build
# stage for efficiency. Target images copy their binary from this image.
# We use go's native cross compilation for multi-arch in this stage, so the
# builder itself is always amd64
FROM --platform=${BUILDPLATFORM} ${GOLANG_IMAGE} AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION

WORKDIR /build
COPY Makefile go.mod go.sum ./
COPY hack/ hack/
COPY cmd/ cmd/
COPY pkg/ pkg/
RUN make build GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOPROXY=${GOPROXY} VERSION=${VERSION}


################################################################################
##                             TARGET IMAGES                                  ##
################################################################################

##
## stackit-csi-plugin
##

# step 1: copy all necessary files from Debian distro to /dest folder
# all magic happens in tools/csi-deps.sh
FROM ${DEBIAN_IMAGE} AS stackit-csi-plugin-utils

RUN clean-install bash rsync mount udev btrfs-progs e2fsprogs xfsprogs util-linux
COPY tools/csi-deps.sh /tools/csi-deps.sh
RUN /tools/csi-deps.sh

# step 2: check if all necessary files are copied and work properly
# the build have to finish without errors, but the result image will not be used
FROM ${DISTROLESS_IMAGE} AS stackit-csi-plugin-utils-check

COPY --from=stackit-csi-plugin-utils /dest /
COPY --from=stackit-csi-plugin-utils /bin/sh /bin/sh
COPY tools/csi-deps-check.sh /tools/csi-deps-check.sh

SHELL ["/bin/sh"]
RUN /tools/csi-deps-check.sh

# step 3: build tiny stackit-csi-plugin image with only necessary files
FROM ${DISTROLESS_IMAGE} AS stackit-csi-plugin

# Copying csi-deps-check.sh simply ensures that the resulting image has a dependency
# on stackit-csi-plugin-utils-check and therefore that the check has passed
COPY --from=stackit-csi-plugin-utils-check /tools/csi-deps-check.sh /bin/csi-deps-check.sh
COPY --from=stackit-csi-plugin-utils /dest /
COPY --from=builder /build/stackit-csi-plugin /bin/stackit-csi-plugin
COPY --from=certs /etc/ssl/certs /etc/ssl/certs

LABEL name="stackit-csi-plugin" \
      license="Apache Version 2.0" \
      maintainers="STACKIT" \
      description="STACKIT CSI Plugin" \
      distribution-scope="public" \
      summary="STACKIT CSI Plugin" \
      help="none"

CMD ["/bin/stackit-csi-plugin"]
