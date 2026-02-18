# Contributing to `cloud-provider-stackit`

Welcome and thank you for making it this far and considering contributing to `cloud-provider-stackit`.
We always appreciate any contributions by raising issues, improving the documentation, fixing bugs or adding new features.

Before opening a PR, please read through this document.

## Process of making an addition

For major changes, API changes or features please open a [Discussion](https://github.com/stackitcloud/cloud-provider-stackit/discussions) or [Issue](https://github.com/stackitcloud/cloud-provider-stackit/issues) beforehand to clarify if this is in line with the project and to avoid unnecessary work.

> Use **Discussions** if it needs to be clarified how to implement or to check if this feature is in line with the project. After all clarifications an issue will be created with the details of the implementation.
>
> Use **Issues** if you have a clear plan how to implement to propose how you would do the change.

To contribute any code to this repository, just do the following:

1. Make sure you have Go's latest version installed
2. Fork this repository
3. Make your changes
   > Please follow the [seven rules of great Git commit messages](https://chris.beams.io/posts/git-commit/#seven-rules)
   > and make sure to keep your commits clean and atomic.
   > Your PR will be squashed before merging so the commits can and should tell a story.
   >
   > Add documentation and tests for your addition if needed.
4. Run `make verify` to ensure your code is ready to be merged
   > If any linting issues occur please fix them.
   > Using a nolint directive should only be used as a last resort.
5. Open a PR and make sure the CI pipelines succeeds.
6. Wait for one of the maintainers to review your code and react to the comments.
7. After approval the maintainers will merge the PR.
8. Thank you for your contribution! :)
