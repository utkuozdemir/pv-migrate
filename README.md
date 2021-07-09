# pv-migrate

[![build](https://github.com/utkuozdemir/pv-migrate/actions/workflows/build.yml/badge.svg)](https://github.com/utkuozdemir/pv-migrate/actions/workflows/build.yml)
[![Coverage Status](https://coveralls.io/repos/github/utkuozdemir/pv-migrate/badge.svg?branch=master)](https://coveralls.io/github/utkuozdemir/pv-migrate?branch=master)
[![Go Report Card](https://goreportcard.com/badge/github.com/utkuozdemir/pv-migrate)](https://goreportcard.com/report/github.com/utkuozdemir/pv-migrate)
![Latest GitHub release](https://img.shields.io/github/release/utkuozdemir/pv-migrate.svg)
[![GitHub license](https://img.shields.io/github/license/utkuozdemir/pv-migrate)](https://github.com/utkuozdemir/pv-migrate/blob/master/LICENSE)
![GitHub stars](https://img.shields.io/github/stars/utkuozdemir/pv-migrate.svg?label=github%20stars)
[![GitHub forks](https://img.shields.io/github/forks/utkuozdemir/pv-migrate)](https://github.com/utkuozdemir/pv-migrate/network)
[![GitHub issues](https://img.shields.io/github/issues/utkuozdemir/pv-migrate)](https://github.com/utkuozdemir/pv-migrate/issues)
![GitHub all releases](https://img.shields.io/github/downloads/utkuozdemir/pv-migrate/total)
![Docker Pulls](https://img.shields.io/docker/pulls/utkuozdemir/pv-migrate)
![SSHD Docker Pulls](https://img.shields.io/docker/pulls/utkuozdemir/pv-migrate-sshd?label=sshd%20-%20docker%20pulls)
![Rsync Docker Pulls](https://img.shields.io/docker/pulls/utkuozdemir/pv-migrate-rsync?label=rsync%20-%20docker%20pulls)

`pv-migrate` is a CLI tool/kubectl plugin to easily migrate 
the contents of one Kubernetes `PersistentVolume[Claim]` to another.

## Introduction

On Kubernetes, if you need to rename a resource (like a `Deployment`) or to move it to a different namespace, 
you can simply create a copy of its manifest with the new namespace and/or name and apply it.

However, it is not as simple with `PersistentVolumeClaim` resources: They are not only metadata,
but they also store data in the underlying storage backend.

In these cases, moving the data stored in the PVC can become a problem, making migrations more difficult.

## Use Cases

- **Case 1:** You have a database that has a PersistentVolumeClaim `db-data` of size `50Gi`.
Time shows that `50Gi` was not enough, and it filled all the disk space.  
And unfortunately, your StorageClass/provisioner doesn't support 
[volume expansion](https://kubernetes.io/blog/2018/07/12/resizing-persistent-volumes-using-kubernetes/).
Now you need to create a new PVC of `1Ti` and somehow copy all the data to the new volume, 
as-is, with its file ownership and permissions.  
Simply create the new PVC `db-data-v2`, and use `pv-migrate` to move data from `db-data` to `db-data-v2`.


- **Case 2:** You need to move PersistentVolumeClaim `my-pvc`  from namespace `ns-a` to namespace `ns-b`.  
Simply create the PVC with the same name and manifest in `ns-b` and use `pv-migrate` to clone its content.


- **Case 3:** You are moving from one cloud provider to another! Now you need to move the database 
from one Kubernetes cluster to the other.  
Both clusters have internet access and the source cluster supports `LoadBalancer` type services with public IPs.  
Just use `pv-migrate` to clone the data **securely over the internet**.
  
## Highlights

- Supports in-namespace, in-cluster as well as cross-cluster migrations
- Uses rsync over SSH with a freshly generated [Ed25519](https://en.wikipedia.org/wiki/EdDSA) 
  or RSA keys each time to securely migrate the files
- Allows specifying your own docker images for rsync and sshd
- Supports multiple migration strategies to do the migration efficiently and fallback to other strategies when needed
- Customizable strategy order
- Supports arm32v7 (Raspberry Pi etc.) and arm64 architectures as well as amd64

## Installation

### Using Homebrew (MacOS/Linux)
If you have homebrew, the installation is as simple as:
```bash
brew tap utkuozdemir/pv-migrate
brew install pv-migrate
```

### Using Scoop (Windows)
If you use [Scoop package manager](https://scoop.sh) on Windows, 
run the following commands in a command prompt (CMD/Powershell):
```powershell
scoop bucket add pv-migrate https://github.com/utkuozdemir/scoop-pv-migrate.git
scoop install pv-migrate/pv-migrate
```

### By downloading the binaries (MacOS/Linux/Windows)

1. Go to the [releases](https://github.com/utkuozdemir/pv-migrate/releases) and download 
   the latest release archive for your platform.
2. Extract the archive.
3. Move the binary to somewhere in your `PATH`.

Sample steps for MacOS:
```bash
$ VERSION=0.5.9
$ wget https://github.com/utkuozdemir/pv-migrate/releases/download/v${VERSION}/pv-migrate_${VERSION}_darwin_x86_64.tar.gz
$ tar -xvzf pv-migrate_${VERSION}_darwin_x86_64.tar.gz
$ mv pv-migrate /usr/local/bin
$ pv-migrate --help
```

### Running directly in Docker container

Alternatively, you can use the 
[official Docker images](https://hub.docker.com/repository/docker/utkuozdemir/pv-migrate) 
that come with the `pv-migrate` binary pre-installed:
```bash
docker run --rm -it utkuozdemir/pv-migrate:0.5.9 pv-migrate migrate ...
```

## Usage

Main command:
```
NAME:
   pv-migrate - A command-line utility to migrate data from one Kubernetes PersistentVolumeClaim to another

USAGE:
   pv-migrate [global options] command [command options] [arguments...]

COMMANDS:
   migrate, m  Migrate data from the source pvc to the destination pvc
   help, h     Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --help, -h     show help (default: false)
   --version, -v  print the version (default: false)
```


Command `migrate`:
```
NAME:
   pv-migrate migrate - Migrate data from the source PVC to the destination PVC

USAGE:
   pv-migrate migrate [command options] [SOURCE_PVC] [DESTINATION_PVC]

OPTIONS:
   --source-kubeconfig value, -k value  Path of the kubeconfig file of the source PVC (default: ~/.kube/config or KUBECONFIG env variable)
   --source-context value, -c value     Context in the kubeconfig file of the source PVC (default: currently selected context in the source kubeconfig)
   --source-namespace value, -n value   Namespace of the source PVC (default: currently selected namespace in the source context)
   --source-mount-read-only, -R         Mount the source PVC in ReadOnly mode (default: true)
   --dest-kubeconfig value, -K value    Path of the kubeconfig file of the destination PVC (default: ~/.kube/config or KUBECONFIG env variable)
   --dest-context value, -C value       Context in the kubeconfig file of the destination PVC (default: currently selected context in the destination kubeconfig)
   --dest-namespace value, -N value     Namespace of the destination PVC (default: currently selected namespace in the destination context)
   --dest-delete-extraneous-files, -d   Delete extraneous files on the destination by using rsync's '--delete' flag (default: false)
   --ignore-mounted, -i                 Do not fail if the source or destination PVC is mounted (default: false)
   --no-chown, -o                       Omit chown on rsync (default: false)
   --strategies value, -s value         The strategies to be used in the given order (default: "mnt2", "svc", "lbsvc")
   --rsync-image value, -r value        Image to use for running rsync (default: "docker.io/utkuozdemir/pv-migrate-rsync:alpine-3-20210522")
   --sshd-image value, -S value         Image to use for running sshd server (default: "docker.io/utkuozdemir/pv-migrate-sshd:alpine-3-20210522")
   --ssh-key-algorithm value, -a value  SSH key algorithm to be used. Valid values are rsa,ed25519 (default: "ed25519")
   --help, -h                           show help (default: false)
```

## Strategies

`pv-migrate` has multiple strategies implemented to carry out the migration operation. Those are the following:

| Name | Description |
| --------- | ----------- |
| `mnt2` | **Mount both** - Mounts both PVCs in a single pod and runs a regular rsync, without using SSH or the network. Only applicable if source and destination PVCs are in the same namespace and both can be mounted from a single pod. |
| `svc` | **Service** - Runs rsync+ssh over a Kubernetes Service (`ClusterIP`). Only applicable when source and destination PVCs are in the same Kubernetes cluster. |
| `lbsvc` | **Load Balancer Service** - Runs rsync+ssh over a Kubernetes Service of type `LoadBalancer`. Always applicable (will fail if `LoadBalancer` IP is not assigned for a long period). |

## Examples

To migrate contents of PersistentVolumeClaim `small-pvc` in namespace `source-ns`
to the PersistentVolumeClaim `big-pvc` in namespace `dest-ns`, use the following command:
```bash
$ pv-migrate migrate \
  --source-namespace source-ns \
  --dest-namespace dest-ns \
  small-pvc big-pvc
```

Full example between different clusters:
```bash
pv-migrate migrate \
  --source-kubeconfig /path/to/source/kubeconfig \
  --source-context some-context \
  --source-namespace source-ns \
  --dest-kubeconfig /path/to/dest/kubeconfig \
  --dest-context some-other-context \
  --dest-namespace dest-ns \
  --dest-delete-extraneous-files \
  old-pvc new-pvc
```

**Note:** For it to run as kubectl plugin via `kubectl pv-migrate ...`, 
put the binary with name `kubectl-pv_migrate` under your `PATH`.

# Contributing

See [CONTRIBUTING](CONTRIBUTING.md) for details.
