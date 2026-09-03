# pv-migrate

[![build](https://img.shields.io/github/actions/workflow/status/utkuozdemir/pv-migrate/build.yml?branch=main&label=build&style=flat-square)](https://github.com/utkuozdemir/pv-migrate/actions/workflows/build.yml)
[![coverage](https://img.shields.io/codecov/c/github/utkuozdemir/pv-migrate/main?style=flat-square)](https://codecov.io/gh/utkuozdemir/pv-migrate)
[![OpenSSF scorecard](https://img.shields.io/ossf-scorecard/github.com/utkuozdemir/pv-migrate?label=openssf%20scorecard&style=flat-square)](https://scorecard.dev/viewer/?uri=github.com/utkuozdemir/pv-migrate)
<!-- OpenSSF best practices: once the project entry exists on bestpractices.dev, add its numeric id here:
[![OpenSSF best practices](https://img.shields.io/cii/level/PROJECT_ID?label=openssf%20best%20practices&style=flat-square)](https://www.bestpractices.dev/projects/PROJECT_ID)
-->
[![latest release](https://img.shields.io/github/v/release/utkuozdemir/pv-migrate?style=flat-square)](https://github.com/utkuozdemir/pv-migrate/releases)
[![license](https://img.shields.io/github/license/utkuozdemir/pv-migrate?style=flat-square)](https://github.com/utkuozdemir/pv-migrate/blob/main/LICENSE)

[![release downloads](https://img.shields.io/github/downloads/utkuozdemir/pv-migrate/total?label=release%20downloads&style=flat-square)](https://github.com/utkuozdemir/pv-migrate/releases)
[![CLI image pulls](https://img.shields.io/docker/pulls/utkuozdemir/pv-migrate?label=cli%20image%20pulls&style=flat-square)](https://hub.docker.com/r/utkuozdemir/pv-migrate)
[![rsync image pulls](https://img.shields.io/docker/pulls/utkuozdemir/pv-migrate-rsync?label=rsync%20image%20pulls&style=flat-square)](https://hub.docker.com/r/utkuozdemir/pv-migrate-rsync)
[![sshd image pulls](https://img.shields.io/docker/pulls/utkuozdemir/pv-migrate-sshd?label=sshd%20image%20pulls&style=flat-square)](https://hub.docker.com/r/utkuozdemir/pv-migrate-sshd)
[![rclone image pulls](https://img.shields.io/docker/pulls/utkuozdemir/pv-migrate-rclone?label=rclone%20image%20pulls&style=flat-square)](https://hub.docker.com/r/utkuozdemir/pv-migrate-rclone)
[![krew](https://img.shields.io/badge/dynamic/yaml?url=https%3A%2F%2Fraw.githubusercontent.com%2Fkubernetes-sigs%2Fkrew-index%2Fmaster%2Fplugins%2Fpv-migrate.yaml&query=%24.spec.version&label=krew&style=flat-square)](https://krew.sigs.k8s.io/plugins/#pv-migrate)

`pv-migrate` is a CLI tool and kubectl plugin that moves the data of Kubernetes `PersistentVolumeClaim`s.
It copies directly from one PVC to another, in the same namespace, across namespaces or across clusters.
It can also back up a PVC to bucket storage (S3-compatible, Azure Blob, GCS or any rclone remote) and restore it later.

---

> [!WARNING]
> Heads up: this is a side project I maintain in my spare time. I might take a long time to look at issues or PRs, or not get to them at all. Sorry in advance, and thanks for understanding.

---

## Demo

Copying a claim into another one:

![A pv-migrate run copying one PersistentVolumeClaim into another, with a progress bar](img/demo.gif)

When a migration fails, the output explains why:

- the exit code the data mover returned, with the meaning that mover's own documentation attaches to it,
- the last lines of the failed pod's log,
- what the cluster reported about the resources involved.

![A pv-migrate run failing, showing the data mover's exit code and what the cluster reported](img/demo-failure.gif)

## Why this exists

On Kubernetes, renaming a resource like a `Deployment` is a manifest change.
You create the same object with a new name or namespace, apply it, and move on.

PVCs are different.
The Kubernetes object is only the metadata.
The data lives in the storage backend, and there is no built-in way to move it.

`pv-migrate` moves that data.
It runs a proven data mover (rsync or rclone) inside the cluster, so nothing is copied through your machine unless you ask for it.

## Quick start

```bash
pv-migrate --source old-pvc --dest new-pvc
```

This copies the contents of `old-pvc` into `new-pvc` in the current namespace, trying the cheapest strategy first.
See [Installation](docs/install.md) for how to get the binary, and [Usage](docs/usage.md) for everything else.

## Workflows

### PVC-to-PVC migration

Copies data directly from one PVC to another with rsync, usually over SSH.
This is the original workflow.

```bash
pv-migrate --source old-pvc --dest new-pvc
```

See [PVC-to-PVC migration](docs/migrate.md) for the strategies and more examples.

### Bucket backup and restore

Backs up a PVC to object storage with rclone and restores it later.
Use it for backups, one-off exports, or moves where direct connectivity between the clusters is not available.

```bash
pv-migrate backup \
  --source app-data \
  --backend s3 \
  --bucket pv-backups \
  --name app-data-2026-04-11

pv-migrate restore \
  --dest app-data-restore \
  --backend s3 \
  --bucket pv-backups \
  --name app-data-2026-04-11
```

See [Bucket backup and restore](docs/backup-restore.md) for the backends, the object layout, raw rclone config mode and the permission caveats.

## Use cases

- A database has a `50Gi` PVC and needs more space, but the storage class does not support [volume expansion](https://kubernetes.io/blog/2018/07/12/resizing-persistent-volumes-using-kubernetes/).
  Create a bigger PVC and copy the data over.
- A PVC has to move from namespace `ns-a` to namespace `ns-b`.
  Create the PVC with the same manifest in `ns-b` and copy its content.
- A workload moves from one cloud provider to another, and the data has to follow it to the new cluster.
  `pv-migrate` copies it over the internet, encrypted with SSH.
- A volume needs another `StorageClass`, e.g., from a `ReadWriteOnce` one like `local-path` to a `ReadWriteMany` one like NFS.
  The storage class is not editable, so create the new PVC and copy.
- A PVC needs a backup in object storage before a risky operation, or its data has to leave the cluster for a later restore.
  `pv-migrate backup` writes it to a bucket, `pv-migrate restore` brings it back.
- Scheduled PVC backups with Kubernetes building blocks only.
  Run `pv-migrate backup` from a `CronJob`, and handle retention with bucket lifecycle rules.
- Direct cluster-to-cluster connectivity is not available or only temporary.
  Back up the source PVC to a bucket, then restore from that bucket into the destination cluster.

## Highlights

- In-namespace, in-cluster and cross-cluster migrations
- rsync over SSH with a freshly generated [Ed25519](https://en.wikipedia.org/wiki/EdDSA) or RSA key pair for every run
- Backup to and restore from S3-compatible storage, Azure Blob, GCS, or any custom rclone remote
- Several migration strategies, tried in order, with fallback:
  - mount both PVCs in a single pod (`mount`)
  - `ClusterIP` service (`clusterip`)
  - `LoadBalancer` service (`loadbalancer`)
  - `NodePort` service (`nodeport`, opt-in)
  - port-forward through the local machine (`local`, opt-in)
- Customizable strategy order
- Push mode (`--rsync-push`) for when the source side cannot expose a service, e.g., behind a firewall or NAT
- Detach mode (`--detach`) for large transfers, so the job keeps running after the CLI exits
- Overrides for the generated manifests through Helm values: images, affinity, resources and everything else the chart exposes
- amd64, arm64 and arm32v7 (Raspberry Pi and similar) binaries and images
- Shell completion for bash, zsh, fish and PowerShell

## Installation

See [docs/install.md](docs/install.md) for the install options (Homebrew, krew, Scoop, release archives, Docker) and shell completion.
The shortest one:

```bash
brew install utkuozdemir/pv-migrate/pv-migrate
```

The artifacts live here:

- [GitHub releases](https://github.com/utkuozdemir/pv-migrate/releases): archives and checksums for Linux, macOS and Windows
- [Docker Hub](https://hub.docker.com/r/utkuozdemir/pv-migrate) and [GHCR](https://github.com/utkuozdemir?tab=packages&repo_name=pv-migrate): the CLI image, next to the three data mover images (`pv-migrate-rsync`, `pv-migrate-sshd`, `pv-migrate-rclone`)
- [krew index](https://krew.sigs.k8s.io/plugins/#pv-migrate), the [Homebrew tap](https://github.com/utkuozdemir/homebrew-pv-migrate) and the [Scoop bucket](https://github.com/utkuozdemir/scoop-pv-migrate)

Releases are signed, and the install guide has the [verification commands](docs/install.md#verifying-what-you-downloaded).

## Usage

See [docs/usage.md](docs/usage.md) for the usage guides and the command reference:

- [PVC-to-PVC migration](docs/migrate.md)
- [Bucket backup and restore](docs/backup-restore.md)
- [CLI reference](docs/cli-reference.md)

## Star history

<!-- markdownlint-disable no-inline-html -->
<a href="https://github.com/utkuozdemir/pv-migrate/stargazers">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/utkuozdemir/star-charts/main/charts/utkuozdemir/pv-migrate/dark.svg" />
    <img alt="Star history of utkuozdemir/pv-migrate" src="https://raw.githubusercontent.com/utkuozdemir/star-charts/main/charts/utkuozdemir/pv-migrate/light.svg" />
  </picture>
</a>
<!-- markdownlint-enable no-inline-html -->

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the process, [.github/SECURITY.md](.github/SECURITY.md) for reporting a security problem, and [.github/GOVERNANCE.md](.github/GOVERNANCE.md) for how the project is run.
The [security model](docs/security-model.md) says what the tool promises and where the trust boundaries are, and the [roadmap](docs/roadmap.md) says what is planned and what is deliberately not.

[AGENTS.md](AGENTS.md) is the project guide for humans and AI assistants working in this repository: how the pieces fit together, and which invariants are easy to break by accident.
