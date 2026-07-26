# Contributing

Contributing to the project is simple. Just do the following:

1. Be nice :)

2. If you are not sure about something (e.g. if something is a bug, on how to solve it, if a feature makes sense etc.),
   before starting to work on it, create an issue for it, so that we can discuss beforehand - maybe saving your time.

3. Fork the repo, do your changes, create a PR.

4. Make sure the build succeeds. Address review feedback if needed.

That's it.

If you are working with an AI assistant, point it at [AGENTS.md](AGENTS.md).
It is the project guide and it is what the assistant needs in order not to break
the things that are easy to break here.

## Building and testing

`Taskfile.yml` is the entry point and mirrors what CI runs:

```bash
task test              # unit tests, including the fuzz seed corpora
task lint              # go, chart, shell and release-config linting
task build             # snapshot build via goreleaser
task test:fuzz         # drive the fuzzers, FUZZTIME=2m to search longer
task test:integration  # migration tests against the current kube context
```

The integration tests create and delete namespaces in a real cluster, so point
them at a throwaway one.

## Creating Releases

To make a release, run:
```bash
task release
```

This will create and push a new version tag, which triggers the release workflow.
The workflow builds and publishes the `pv-migrate` CLI binary, Docker image,
and the `pv-migrate-rsync` and `pv-migrate-sshd` images — all with the same version tag.

## Editing the helm chart

The `pv-migrate` helm chart is located at `internal/helm/pv-migrate`. It is embedded into the Go binary during build.

The chart README is generated from the comments in `values.yaml` by
[helm-docs](https://github.com/norwoodj/helm-docs), so never edit it directly.
After changing the chart, regenerate it (needs docker):

```bash
task generate-helm-chart-docs
```

`task generate-all` regenerates the chart README and the CLI reference together,
which is what CI checks for dirtiness.
