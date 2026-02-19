# Release Procedure

## Table of Contents

- [Overview](#overview)
- [General Information](#general-information)
- [Automated Release Process (Primary Method)](#automated-release-process-primary-method)
- [Manual Release Process (Fallback Method)](#manual-release-process-fallback-method)

## Overview

This document outlines the standard procedure for creating new releases of the Cloud Provider STACKIT. Cloud Provider releases are synchronized with kubernetes/kubernetes releases. Minor versions may be released as needed for critical bug fixes.

## General Information

- **Branching Strategy:** All releases are created from `release-*` branches, which are tied to specific Kubernetes minor versions. For example, all Cloud Provider releases compatible with Kubernetes `v1.33` are cut from the `release-v1.33` branch.
- **Versioning:** Cloud Provider versioning follows the format `vMAJOR.MINOR.PATCH` (e.g., `v1.33.0`, `v1.33.1`), where:
  - `MAJOR.MINOR` matches the Kubernetes version from the release branch
  - `PATCH` is incremented for each subsequent release within the same Kubernetes version
- **CI/CD System:** All release and image builds are managed by our **Prow CI** infrastructure.

## Automated Release Process (Primary Method)

The primary release method is automated using a tool called `release-tool`. This process is designed to be straightforward and require minimal manual intervention.

1. **Draft Creation:** On every successful merge (post-submit) to a `release-*` branch, a Prow job automatically runs the `release-tool`. This tool creates a new draft release on GitHub or updates the existing one with a changelog generated from recent commits.
2. **Publishing the Release:** When the draft is ready, navigate to the repository's "Releases" page on GitHub. Locate the draft, review the changelog, and publish it by clicking the "Publish release" button.

Publishing the release automatically creates the corresponding Git tag (e.g., `v1.33.1`), which triggers a separate Prow job to build the final container images and attach them to the GitHub release.

## Manual Release Process (Fallback Method)

If the `release-tool` or its associated Prow job fails, use the GitHub web UI to create and publish a release:

1. Go to the repository on GitHub and click **Releases** on the right side, then click **Draft new release**.

2. Open the **Select tag** dropdown and choose **Create new tag** at the bottom. Enter the new tag name (for example `v2.1.0`) and pick the correct `release-` as target branch, then confirm.

3. Click **Generate release notes** to let GitHub populate the changelog.

4. In the release description, add a line `Released by @<your github handle>` to indicate the publisher.

5. Click **Publish release** to create the release.

Publishing a new release triggers the same Prow release job that builds and publishes the final container images.

## Releasing a New Kubernetes Version

1. Create a new version of the [`cloud-provider`](https://github.com/stackitcloud/cloud-provider) fork.
  1. Create a new release branch `ske-v0.xx` from the latest patch version tag of the desired Kubernetes version.
  2. Cherry-pick our changes into the release branch. Example: https://github.com/stackitcloud/cloud-provider/commits/ske-v0.35/
  3. Tag the release branch with `v0.xx.x-ske-x`.
2. Bump the Kubernetes packages, including an overwrite of the `cloud-provider` fork, in the `main` branch.
3. Create a new release branch `release-v1.XX` from `main`.
4. Update the Renovate configuration via a PR against `main`: add the new release branch to `baseBranchPatterns` and add a new rule in `packageRules` for `/^k8s.io/`. Cherry-pick this PR to all maintained release branches. Example PR: https://github.com/stackitcloud/cloud-provider-stackit/pull/756
5. Create a manual release for the new branch (note: the release tool will only work if there is already a tag on the branch). Do not generate release notes as they will contain many unrelated changes. Just add `Initial release of v1.xx.0` as release notes.
