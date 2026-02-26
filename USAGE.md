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
      --dest string                     Destination PVC name
  -C, --dest-context string             Context in the kubeconfig file of the destination PVC
  -d, --dest-delete-extraneous-files    Delete extraneous files on the destination using rsync's --delete flag
  -H, --dest-host-override string       Override for the rsync destination host over SSH. By default, determined by the strategy. Has no effect for the mount and local strategies
  -K, --dest-kubeconfig string          Path of the kubeconfig file of the destination PVC
  -N, --dest-namespace string           Namespace of the destination PVC
  -P, --dest-path string                Filesystem path to migrate in the destination PVC (default "/")
      --helm-set strings                Additional Helm values (key1=val1,key2=val2)
      --helm-set-file strings           Additional Helm values from files (key1=path1,key2=path2)
      --helm-set-string strings         Additional Helm string values (key1=val1,key2=val2)
  -t, --helm-timeout duration           Helm install/uninstall timeout (default 1m0s)
  -f, --helm-values strings             Additional Helm values files (YAML file or URL, can specify multiple)
  -h, --help                            help for pv-migrate
  -i, --ignore-mounted                  Do not fail if the source or destination PVC is mounted
      --loadbalancer-timeout duration   Timeout for the load balancer to receive an external IP. Only used by the loadbalancer strategy (default 2m0s)
      --log-format string               Log format, one of text, json (default "text")
      --log-level string                Log level, one of DEBUG, INFO, WARN, ERROR or an slog-parseable level: https://pkg.go.dev/log/slog#Level.UnmarshalText (default "INFO")
  -o, --no-chown                        Omit chown during rsync
  -x, --no-cleanup                      Do not clean up after migration
      --no-compress                     Do not compress data during migration (disables rsync -z)
  -b, --show-progress-bar               Show a progress bar during migration (default true if stderr is a TTY)
      --source string                   Source PVC name
  -c, --source-context string           Context in the kubeconfig file of the source PVC
  -k, --source-kubeconfig string        Path of the kubeconfig file of the source PVC
  -R, --source-mount-read-write         Mount the source PVC in read-write mode
  -n, --source-namespace string         Namespace of the source PVC
  -p, --source-path string              Filesystem path to migrate in the source PVC (default "/")
  -a, --ssh-key-algorithm string        SSH key algorithm, one of rsa, ed25519 (default "ed25519")
  -s, --strategies strings              Comma-separated list of strategies in order (available: mount, clusterip, loadbalancer, nodeport, local) (default [mount,clusterip,loadbalancer])
  -v, --version                         Version for pv-migrate

Use "pv-migrate [command] --help" for more information about a command.
```

The Kubernetes resources created by pv-migrate are sourced from a [Helm chart](internal/helm/pv-migrate).

You can pass raw values to the backing Helm chart
using the `--helm-*` flags for further customization: container images,
resources, serviceacccounts, additional annotations etc.

## Strategies

`pv-migrate` has multiple strategies implemented to carry out the migration operation. Those are the following:

| Name       | Description |
|------------|-------------|
| `mnt2`     | **Mount both** - Mounts both PVCs in a single pod and runs a regular rsync, without using SSH or the network. Only applicable if source and destination PVCs are in the same namespace and both can be mounted from a single pod. |
| `svc`      | **Service** - Runs rsync+ssh over a Kubernetes Service (`ClusterIP`). Only applicable when source and destination PVCs are in the same Kubernetes cluster. |
| `lbsvc`    | **Load Balancer Service** - Runs rsync+ssh over a Kubernetes Service of type `LoadBalancer`. Always applicable (will fail if `LoadBalancer` IP is not assigned for a long period). |
| `nodeport` | **NodePort Service** - Runs rsync+ssh over a Kubernetes Service of type `NodePort`. Not enabled by default. A custom NodePort can be specified via `--helm-set sshd.service.nodePort=<port>`. |
| `local`    | **Local Transfer** - Runs sshd on both source and destination, then uses a combination of `kubectl port-forward` logic and an SSH reverse proxy to tunnel all the traffic over the client device (the device which runs pv-migrate, e.g. your laptop). Requires `ssh` command to be available on the client device. <br/><br/>Note that this strategy is **experimental** (and not enabled by default), potentially can put heavy load on both apiservers and is not as resilient as others. It is recommended for small amounts of data and/or when the only access to both clusters seems to be through `kubectl` (e.g. for air-gapped clusters, on jump hosts etc.). |

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

### Example 7: Using the NodePort strategy with a specific port

```bash
$ pv-migrate \
  --strategies nodeport \
  --helm-set sshd.service.nodePort=30555 \
  --source old-pvc \
  --dest new-pvc
```

**For further customization on the rendered manifests**
(custom labels, annotations, etc.), see the [Helm chart values](internal/helm/pv-migrate).
