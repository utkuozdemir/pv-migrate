# pv-migrate

[![build](https://github.com/utkuozdemir/pv-migrate/actions/workflows/build.yml/badge.svg)](https://github.com/utkuozdemir/pv-migrate/actions/workflows/build.yml)
[![Coverage Status](https://coveralls.io/repos/github/utkuozdemir/pv-migrate/badge.svg?branch=master)](https://coveralls.io/github/utkuozdemir/pv-migrate?branch=master)
[![Go Report Card](https://goreportcard.com/badge/github.com/utkuozdemir/pv-migrate)](https://goreportcard.com/report/github.com/utkuozdemir/pv-migrate)
![Latest GitHub release](https://img.shields.io/github/release/utkuozdemir/pv-migrate.svg)
[![GitHub license](https://img.shields.io/github/license/utkuozdemir/pv-migrate)](https://github.com/utkuozdemir/pv-migrate/blob/master/LICENSE)
![GitHub stars](https://img.shields.io/github/stars/utkuozdemir/pv-migrate.svg?label=github%20stars)
[![GitHub forks](https://img.shields.io/github/forks/utkuozdemir/pv-migrate)](https://github.com/utkuozdemir/pv-migrate/network)
[![GitHub issues](https://img.shields.io/github/issues/utkuozdemir/pv-migrate)](https://github.com/utkuozdemir/pv-migrate/issues)

This is a CLI tool/kubectl plugin to easily migrate 
the contents of one Kubernetes `PersistentVolume` to another.

Common use case: You have a database with a bound 50gb PersistentVolumeClaim.
Unfortunately 50gb was not enough, and you filled all the disk space rather quickly. 
Sadly, your StorageClass/provisioner doesn't support 
[volume expansion](https://kubernetes.io/blog/2018/07/12/resizing-persistent-volumes-using-kubernetes/).
Now you need to create a new PVC of 1tb and somehow copy all the data to the new volume, 
as-is, with its permissions and so on.

Another use case: You need to move a PersistentVolumeClaim from one namespace to another.

## Installation

1. Go to the [releases](https://github.com/utkuozdemir/pv-migrate/releases) and download 
   the latest release archive for your platform.
2. Extract the archive.
3. Move the binary to somewhere in your `PATH`.

Steps for MacOS:
```bash
$ VERSION=v0.4.0
$ wget https://github.com/utkuozdemir/pv-migrate/releases/download/${VERSION}/pv-migrate_${VERSION}_darwin_x86_64.tar.gz
$ tar -xvzf pv-migrate_${VERSION}_darwin_x86_64.tar.gz
$ mv pv-migrate /usr/local/bin
$ pv-migrate --help
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
   pv-migrate migrate - Migrate data from the source pvc to the destination pvc

USAGE:
   pv-migrate migrate [command options] [SOURCE_PVC] [DESTINATION_PVC]

OPTIONS:
   --source-kubeconfig value, -k value    Path of the kubeconfig file of the source pvc (default: ~/.kube/config or KUBECONFIG env variable)
   --source-context value, -c value       Context in the kubeconfig file of the source pvc (default: currently selected context in the source kubeconfig)
   --source-namespace value, -n value     Namespace of the source pvc (default: currently selected namespace in the source context)
   --dest-kubeconfig value, -K value      Path of the kubeconfig file of the destination pvc (default: ~/.kube/config or KUBECONFIG env variable)
   --dest-context value, -C value         Context in the kubeconfig file of the destination pvc (default: currently selected context in the destination kubeconfig)
   --dest-namespace value, -N value       Namespace of the destination pvc (default: currently selected namespace in the destination context)
   --dest-delete-extraneous-files, -d     Delete extraneous files on the destination by using rsync's '--delete' flag (default: false)
   --ignore-mounted, -i                   Do not fail if the source or destination PVC is mounted (default: false)
   --no-chown, -o                         Omit chown on rsync (default: false)
   --override-strategies value, -s value  Override the default list of strategies and their order by priority
   --rsync-image value, -r value          Image to use for running rsync (default: "docker.io/instrumentisto/rsync-ssh:alpine")
   --sshd-image value, -S value           Image to use for running sshd server (default: "docker.io/panubo/sshd:1.3.0")
   --help, -h                             show help (default: false)
```

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
