# Usage

Root command:

```
Migrate data from one Kubernetes PersistentVolumeClaim to another

Usage:
  pv-migrate [--source-namespace=<source-ns>] --source=<source-pvc> [--dest-namespace=<dest-ns>] --dest=<dest-pvc> [flags]
  pv-migrate [command]

Available Commands:
  completion  Generate completion script
  help        Help about any command

Flags:
      --compress                       compress data during migration ('-z' flag of rsync) (default true)
      --dest string                    destination PVC name
  -C, --dest-context string            context in the kubeconfig file of the destination PVC
  -d, --dest-delete-extraneous-files   delete extraneous files on the destination by using rsync's '--delete' flag
  -H, --dest-host-override string      the override for the rsync host destination when it is run over SSH, in cases when you need to target a different destination IP on rsync for some reason. By default, it is determined by used strategy and differs across strategies. Has no effect for mnt2 and local strategies
  -K, --dest-kubeconfig string         path of the kubeconfig file of the destination PVC
  -N, --dest-namespace string          namespace of the destination PVC
  -P, --dest-path string               the filesystem path to migrate in the destination PVC (default "/")
      --helm-set strings               set additional Helm values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)
      --helm-set-file strings          set additional Helm values from respective files specified via the command line (can specify multiple or separate values with commas: key1=path1,key2=path2)
      --helm-set-string strings        set additional Helm STRING values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)
  -t, --helm-timeout duration          install/uninstall timeout for helm releases (default 1m0s)
  -f, --helm-values strings            set additional Helm values by a YAML file or a URL (can specify multiple)
  -h, --help                           help for pv-migrate
  -i, --ignore-mounted                 do not fail if the source or destination PVC is mounted
      --lbsvc-timeout duration         timeout for the load balancer service to receive an external IP. Only used by the lbsvc strategy (default 2m0s)
      --log-format string              log format, must be one of: text, json (default "text")
      --log-level string               log level, must be one of "DEBUG, INFO, WARN, ERROR" or an slog-parseable level: https://pkg.go.dev/log/slog#Level.UnmarshalText (default "INFO")
  -o, --no-chown                       omit chown on rsync
  -b, --no-progress-bar                do not display a progress bar
      --nodeport-port                  defines custom NodePort to use. Only used by the nodeport strategy
  -x, --skip-cleanup                   skip cleanup of the migration
      --source string                  source PVC name
  -c, --source-context string          context in the kubeconfig file of the source PVC
  -k, --source-kubeconfig string       path of the kubeconfig file of the source PVC
  -R, --source-mount-read-only         mount the source PVC in ReadOnly mode (default true)
  -n, --source-namespace string        namespace of the source PVC
  -p, --source-path string             the filesystem path to migrate in the source PVC (default "/")
  -a, --ssh-key-algorithm string       ssh key algorithm to be used. Valid values are rsa,ed25519 (default "ed25519")
  -s, --strategies strings             the comma-separated list of strategies to be used in the given order (available: mnt2, svc, lbsvc, nodeport, local) (default [mnt2,svc,lbsvc,nodeport])
  -v, --version                        version for pv-migrate

Use "pv-migrate [command] --help" for more information about a command.
```

The Kubernetes resources created by pv-migrate are sourced from a [Helm chart](helm/pv-migrate).

You can pass raw values to the backing Helm chart
using the `--helm-*` flags for further customization: container images,
resources, serviceacccounts, additional annotations etc.

## Strategies

`pv-migrate` has multiple strategies implemented to carry out the migration operation. Those are the following:

| Name    | Description                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
|---------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `mnt2`  | **Mount both** - Mounts both PVCs in a single pod and runs a regular rsync, without using SSH or the network. Only applicable if source and destination PVCs are in the same namespace and both can be mounted from a single pod.                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `svc`   | **Service** - Runs rsync+ssh over a Kubernetes Service (`ClusterIP`). Only applicable when source and destination PVCs are in the same Kubernetes cluster.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `lbsvc` | **Load Balancer Service** - Runs rsync+ssh over a Kubernetes Service of type `LoadBalancer`. Always applicable (will fail if `LoadBalancer` IP is not assigned for a long period).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `nodeport` | **NodePort Service** - Runs rsync+ssh over a Kubernetes Service of type `NodePort`. Creates a service that opens a port between 30000-32767 on a single random node. The `--nodeport-port` flag can be specified if you require usage of a specific port.                                                                                                                                                                                                                                                                                                                                                                |
| `local` | **Local Transfer** - Runs sshd on both source and destination, then uses a combination of `kubectl port-forward` logic and an SSH reverse proxy to tunnel all the traffic over the client device (the device which runs pv-migrate, e.g. your laptop). Requires `ssh` command to be available on the client device. <br/><br/>Note that this strategy is **experimental** (and not enabled by default), potentially can put heavy load on both apiservers and is not as resilient as others. It is recommended for small amounts of data and/or when the only access to both clusters seems to be through `kubectl` (e.g. for air-gapped clusters, on jump hosts etc.). |

## Examples

See the various examples below which copy the contents of the `old-pvc` into the `new-pvc`.

### Example 1: In a single namespace (minimal example)

```bash
$ pv-migrate --source old-pvc --dest new-pvc
```

### Example 2: Between namespaces

```bash
$ pv-migrate \
  --source-namespace source-ns --source old-pvc \
  --dest-namespace dest-ns --dest new-pvc
```

### Example 3: Between different clusters

```bash
pv-migrate \
  --source-kubeconfig /path/to/source/kubeconfig \
  --source-context some-context \
  --source-namespace source-ns \
  --source old-pvc \
  --dest-kubeconfig /path/to/dest/kubeconfig \
  --dest-context some-other-context \
  --dest-namespace dest-ns \
  --dest-delete-extraneous-files \
  --dest new-pvc
```

### Example 4: Using custom container images from custom repository

```bash
$ pv-migrate \
  --helm-set rsync.image.repository=mycustomrepo/rsync \
  --helm-set rsync.image.tag=v1.2.3 \
  --helm-set sshd.image.repository=mycustomrepo/sshd \
  --helm-set sshd.image.tag=v1.2.3 \
  --source old-pvc \
  --dest new-pvc
```

### Example 5: Enabling network policies (on clusters with deny-all traffic rules)

```bash
$ pv-migrate \
  --helm-set sshd.networkPolicy.enabled=true \
  --helm-set rsync.networkPolicy.enabled=true \
  --source-namespace source-ns --source old-pvc \
  --dest-namespace dest-ns --dest new-pvc
```

### Example 6: Passing additional rsync arguments

```bash
$ pv-migrate \
  --helm-set rsync.extraArgs="--partial --inplace" \
  --source old-pvc --dest new-pvc
```

### Example 7: Using a specific NodePort for the NodePort strategy

```bash
$ pv-migrate \
  --strategies nodeport \
  --nodeport-port 30555 \
  --source old-pvc \
  --dest new-pvc
```

**For further customization on the rendered manifests**
(custom labels, annotations, etc.), see the [Helm chart values](helm/pv-migrate).
