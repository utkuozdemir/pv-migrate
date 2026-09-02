# Contributing

Contributing is simple:

1. Be nice :) The [code of conduct](CODE_OF_CONDUCT.md) applies.
2. If you are not sure about something (e.g., whether it is a bug, how to solve it, whether a feature makes sense), open an issue first so we can discuss it. It may save your time.
3. Fork the repo, make your changes, open a pull request.
4. Add tests for what you change. New functionality comes with tests, and a bug fix comes with a test that would have caught it.
5. Sign off your commits with `git commit -s`. This is the [Developer Certificate of Origin](https://developercertificate.org/): you state that you have the right to contribute the code.
6. Make sure the CI build is green. Address the review comments if there are any.

That's it.
One exception: security problems do not go to the issue tracker, see [SECURITY.md](.github/SECURITY.md).

If you are working with an AI assistant, point it at [AGENTS.md](AGENTS.md).
It is the project guide, and it is what the assistant needs in order not to break the things that are easy to break here.

## Setting up for development

You need Go (the version pinned in `go.mod`), Docker and [Task](https://taskfile.dev).

```bash
git clone https://github.com/utkuozdemir/pv-migrate.git
cd pv-migrate
go build ./...
go test ./...
```

`Taskfile.yml` is the entry point and mirrors what CI runs:

```bash
task test              # unit tests, including the fuzz seed corpora
task lint              # go, chart, shell and release-config linting
task fmt               # formatters
task build             # snapshot build via goreleaser
task test:fuzz         # drive the fuzzers, FUZZTIME=2m to search longer
task test:integration  # migration tests against the current kube context
```

Every linter and the tests run in containers, at the versions pinned in `hack/dev.Dockerfile`.
This way, the workstation and CI run the same tool versions.

The unit tests need no cluster.
The integration tests create and delete namespaces in a real cluster, so point them at a throwaway one.

## Code style

- Go code is formatted with `gofumpt` and checked by `golangci-lint` with all linters enabled. Each disabled linter has a written reason in `.golangci.yml`.
- Shell scripts go through `shfmt`.
- `task lint` runs all of it, and the CI build fails on any finding.

Commit messages follow the conventional commit format, `type(scope): description`, with a body that explains what changed and why.
The changelog is generated from them, grouped by type.

## Editing the Helm chart

The chart is at `internal/helm/pv-migrate` and is embedded into the binary at build time, so a template change is a code change.

The chart README is generated from the comments in `values.yaml` by [helm-docs](https://github.com/norwoodj/helm-docs), so never edit it directly.
`docs/cli-reference.md` is generated too, from the commands' help output.
Regenerate both after a change, CI fails on a stale copy:

```bash
task generate-all
```

## Creating releases

Releases are cut by the maintainer:

```bash
task release
```

It needs Task, Docker ([svu](https://github.com/caarlos0/svu) runs containerized) and the [GitHub CLI](https://cli.github.com/).
The version is derived from the commit history.
The tag is signed, and the release workflow refuses to build from a tag that GitHub does not show as verified.
