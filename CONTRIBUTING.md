# Contributing

Contributing to the project is simple. Just do the following:

1. Be nice :)

2. If you are not sure about something (e.g. if something is a bug, on how to solve it, if a feature makes sense etc.),
   before starting to work on it, create an issue for it, so that we can discuss beforehand - maybe saving your time.

3. Fork the repo, do your changes, create a PR.

4. Make sure the build succeeds. Do the changes from after the review if needed.

That's it.

## Creating Releases

- To make a release of `pv-migrate` itself, run the following command:
  ```bash
  task release
  ```

- To make a release of docker images, use their special tag prefixes, followed by the version. Example:
  ```bash
  git tag -a "docker-sshd-1.0.0" -m "Docker Sshd Image Release 1.0.0"
  git tag -a "docker-rsync-1.0.0" -m "Docker Rsync Image Release 1.0.0"
  git push
  push --tags
  ```
  These will result in the Docker images with the following tags being pushed:
  ```
  docker.io/utkuozdemir/pv-migrate-sshd:1.0.0
  docker.io/utkuozdemir/pv-migrate-rsync:1.0.0
  ```

## Editing the helm chart

The `pv-migrate` helm chart is located at `helm/pv-migrate`. It is inserted into the go code during build. 
The source is a helm package located in `internal/migrator/helm-chart.tgz`.

If you want to tweak the helm chart, you must run the following command before recompiling the code in order
to update the chart (you need [helm](https://helm.sh/docs/intro/install/) and [helm-docs](https://github.com/norwoodj/helm-docs) installed):

```bash
task update-chart
```
