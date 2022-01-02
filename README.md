# pv-migrate

[![build](https://github.com/utkuozdemir/pv-migrate/actions/workflows/lint-build-test.yml/badge.svg)](https://github.com/utkuozdemir/pv-migrate/actions/workflows/lint-build-test.yml)
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
the contents of one Kubernetes `PersistentVolumeClaim` to another.

## Demo

![pv-migrate demo GIF](img/demo.gif)

## Introduction

On Kubernetes, if you need to rename a resource (like a `Deployment`) or to move it to a different namespace, 
you can simply create a copy of its manifest with the new namespace and/or name and apply it.

However, it is not as simple with `PersistentVolumeClaim` resources: They are not only metadata,
but they also store data in the underlying storage backend.

In these cases, moving the data stored in the PVC can become a problem, making migrations more difficult.

## Use Cases

:arrow_right: You have a database that has a PersistentVolumeClaim `db-data` of size `50Gi`.  
Your DB grew over time, and you need more space for it.  
You cannot resize the PVC because it doesn't support [volume expansion](https://kubernetes.io/blog/2018/07/12/resizing-persistent-volumes-using-kubernetes/).  
Simply create a new, bigger PVC `db-data-v2` and use `pv-migrate` to copy data from `db-data` to `db-data-v2`.


:arrow_right: You need to move PersistentVolumeClaim `my-pvc`  from namespace `ns-a` to namespace `ns-b`.  
Simply create the PVC with the same name and manifest in `ns-b` and use `pv-migrate` to copy its content.


:arrow_right: You are moving from one cloud provider to another, 
and you need to move the data from one Kubernetes cluster to the other.  
Just use `pv-migrate` to copy the data **securely over the internet**.

## Highlights

- Supports in-namespace, in-cluster as well as cross-cluster migrations
- Uses rsync over SSH with a freshly generated [Ed25519](https://en.wikipedia.org/wiki/EdDSA) 
  or RSA keys each time to securely migrate the files
- Allows full customization of the manifests (e.g. specifying your own docker images for rsync and sshd, configuring affinity etc.)
- Supports multiple migration strategies to do the migration efficiently and fallback to other strategies when needed
- Customizable strategy order
- Supports arm32v7 (Raspberry Pi etc.) and arm64 architectures as well as amd64
- Supports completion for popular shells: bash, zsh, fish, powershell

## Installation

See [INSTALL.md](INSTALL.md) for various installation methods and shell completion configuration.

## Usage

See [USAGE.md](USAGE.md) for the CLI reference and examples.


# Stargazers over time

[![Stargazers over time](https://starchart.cc/utkuozdemir/pv-migrate.svg)](https://starchart.cc/utkuozdemir/pv-migrate)


# Contributing

See [CONTRIBUTING](CONTRIBUTING.md) for details.
