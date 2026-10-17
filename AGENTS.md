# Agent instructions

## Versioning

Every pull request bumps this repository's version in the same PR: patch for a fix or a refactor, minor for a feature or a visible change, major for a breaking change. Stacked PRs bump in order. Put the new version in the PR description and state the bump (for example `1.0.0 → 1.0.1`).

Where the version lives: release git tags (`vX.Y.Z`), the Go module version. State the next tag in the PR description.
