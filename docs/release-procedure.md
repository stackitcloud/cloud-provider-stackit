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

---

## Automated Release Process (Primary Method)

The primary release method is automated using a tool called `release-tool`. This process is designed to be straightforward and require minimal manual intervention.

1. **Draft Creation:** On every successful merge (post-submit) to a `release-*` branch, a Prow job automatically runs the `release-tool`. This tool creates a new draft release on GitHub or updates the existing one with a changelog generated from recent commits.
2. **Publishing the Release:** When the draft is ready, navigate to the repository's "Releases" page on GitHub. Locate the draft, review the changelog, and publish it by clicking the "Publish release" button.

Publishing the release automatically creates the corresponding Git tag (e.g., `v1.33.1`), which triggers a separate Prow job to build the final container images and attach them to the GitHub release.

---

## Manual Release Process (Fallback Method)

If the `release-tool` or its associated Prow job fails, you can manually trigger a release by creating and pushing a Git tag from the appropriate release branch.

1. **Check out the release branch:** Ensure you have the latest changes from the correct release branch.

   ```bash
   git checkout release-v1.33.x
   git pull origin release-v1.33.x
   ```

2. **Create the Git tag:** Create a new, annotated tag for the release, following semantic versioning.

   ```bash
   # Example for creating version v1.33.1
   git tag v1.33.1
   ```

3. **Push the tag to the remote repository:**

   ```bash
   # Example for pushing tag v1.33.1
   git push origin v1.33.1
   ```

Pushing a tag that starts with `v` (e.g., `v1.33.1`) automatically triggers the same Prow release job that builds and publishes the final container images. You may need to manually update the release notes on GitHub afterward.
