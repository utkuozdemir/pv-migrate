# Contributing

Contributing to the project is simple. Just do the following:

1. Be nice :)

2. If you are not sure about something (e.g. if something is a bug, on how to solve it, if a feature makes sense etc.),
   before starting to work on it, create an issue for it, so that we can discuss beforehand - maybe saving your time.

3. Fork the repo, do your changes, create a PR.

4. Make sure the build succeeds. Address review feedback if needed.

That's it.

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

If you want to tweak the helm chart, you must run the following command before recompiling the code in order
to update the chart (you need [helm](https://helm.sh/docs/intro/install/) and [helm-docs](https://github.com/norwoodj/helm-docs) installed):

```bash
task update-chart
```
