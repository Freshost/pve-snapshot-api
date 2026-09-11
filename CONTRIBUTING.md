# Contributing

Open an issue for substantial changes. Submit focused pull requests against
`main`, describing the problem, behavior changes and validation.

Use the Go version in `go.mod`; run `gofmt -w cmd pkg`, `go vet ./...`,
`go test -race ./...` and `make release-check`. Storage lifecycle changes also
need native ZFS tests; see [compatibility](docs/compatibility.md).

Add regression coverage and document API or upgrade changes. Use synthetic test
data and example addresses. Do not publish credentials, private infrastructure
details, deployment logs or internal review notes in code, issues or PRs.
Report vulnerabilities privately through [SECURITY.md](SECURITY.md).

Contributions use the project's Apache-2.0 license. See [releasing](docs/releasing.md)
for the maintainer workflow.
