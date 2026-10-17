# Releasing the Go SDK

The module path is `github.com/nozle-dev/nozle-go`. Go resolves versions from immutable `vMAJOR.MINOR.PATCH` Git tags; `go.mod` does not contain a package version. The first release is `v0.9.0`.

1. Merge the SDK branch into the public `nozle-dev/nozle-go` repository. Review the matching entry in `CHANGELOG.md` and set its release date.
2. Run `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./examples/...`, and `go test ./...` inside `examples/provider-auto`.
3. Create and push an annotated tag from the verified commit: `git tag -a vX.Y.Z -m 'Go SDK X.Y.Z'` and `git push origin vX.Y.Z`.
4. The tag workflow verifies the version, repeats the checks, and creates the GitHub release from the changelog entry.
5. After the workflow succeeds and the module is public, verify `GOPROXY=https://proxy.golang.org go list -m github.com/nozle-dev/nozle-go@vX.Y.Z` from a separate directory.

For later releases, update the changelog, use the next semantic version tag, and repeat the same steps. Never move an existing tag; publish a new patch version for corrections.
