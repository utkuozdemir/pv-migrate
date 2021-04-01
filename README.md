# pv-migrate

[![Continuous Integration](https://github.com/utkuozdemir/pv-migrate/actions/workflows/ci.yml/badge.svg)](https://github.com/utkuozdemir/pv-migrate/actions/workflows/ci.yml)
[![Coverage Status](https://coveralls.io/repos/github/utkuozdemir/pv-migrate/badge.svg?branch=master)](https://coveralls.io/github/utkuozdemir/pv-migrate?branch=master)
![Latest GitHub release](https://img.shields.io/github/release/utkuozdemir/pv-migrate.svg)
[![GitHub license](https://img.shields.io/github/license/utkuozdemir/pv-migrate)](https://github.com/utkuozdemir/pv-migrate/blob/master/LICENSE)
![GitHub stars](https://img.shields.io/github/stars/utkuozdemir/pv-migrate.svg?label=github%20stars)
[![GitHub forks](https://img.shields.io/github/forks/utkuozdemir/pv-migrate)](https://github.com/utkuozdemir/pv-migrate/network)
[![GitHub issues](https://img.shields.io/github/issues/utkuozdemir/pv-migrate)](https://github.com/utkuozdemir/pv-migrate/issues)

This is a cli tool/kubectl plugin to easily migrate 
the contents of one Kubernetes `PersistentVolume` to another.

Common use case: You have a database with a bound 50gb PersistentVolumeClaim.
Unfortunately 50gb was not enough, and you filled all the disk space rather quickly. 
Sadly, your StorageClass/provisioner doesn't support [volume expansion](https://kubernetes.io/blog/2018/07/12/resizing-persistent-volumes-using-kubernetes/).
Now you need to create a new PVC of 1tb and somehow copy all the data to the new volume, as-is, with its permissions and so on.

Another use case: You need to move a PersistentVolumeClaim from one namespace to another.

## Usage

To migrate contents of PersistentVolumeClaim small-pvc in namespace ns-a
to the PersistentVolumeClaim big-pvc in namespace ns-b, use the following command:
```bash
$ kubectl pv-migrate \
  --source-namespace ns-a \
  --source small-pvc \
  --dest-namespace ns-b \
  --dest big-pvc
```
This will create a temporary **sshd** pod that has the `small-pvc` mounted, 
and an **rsync** job with `big-pvc` mounted, and will rsync the whole content from the source to the target.
It will clean up the temporary resources it created after the operation is completed (or failed).

The output will be like the following:
```
INFO[0000] Both claims exist and bound, proceeding...
INFO[0000] Creating sshd pod                             podName=pv-migrate-sshd-amcsl
INFO[0000] Waiting for pod to start running              podName=pv-migrate-sshd-amcsl
INFO[0010] sshd pod running                              podName=pv-migrate-sshd-amcsl
INFO[0010] Creating rsync job                            jobName=pv-migrate-rsync-amcsl
INFO[0010] Waiting for rsync job to finish               jobName=pv-migrate-rsync-amcsl
INFO[0016] Job is running                                jobName=pv-migrate-rsync-amcsl podName=pv-migrate-rsync-amcsl-9cff6
INFO[0017] Job completed...                              jobName=pv-migrate-rsync-amcsl podName=pv-migrate-rsync-amcsl-9cff6
INFO[0017] Doing cleanup                                 instance=amcsl namespace=ns-a
INFO[0018] Finished cleanup                              instance=amcsl
INFO[0018] Doing cleanup                                 instance=amcsl namespace=ns-b
INFO[0018] Finished cleanup                              instance=amcsl
```


**Note:** For it to run as kubectl plugin via `kubectl pv-migrate`, 
put the binary with name `kubectl-pv_migrate` under your `PATH`.  
To use it standalone, simply run it like `./pv-migrate --source-namespace ....`

## Building

To build for your platform
```bash
$ make build
```

To build for all major platforms and prepare release archives:
```bash
$ make build-all
```

## Notes

* This version has a **hardcoded RSA public/private 
key pair** in the sshd/rsync docker images and in the codebase. 
This is intentional, since security is not a concern at this release.
In the future, a key pair will probably be generated on the client and be used instead.
