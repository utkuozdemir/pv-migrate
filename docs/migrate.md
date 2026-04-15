# PVC-To-PVC Migration

PVC-to-PVC migration copies data directly from one Kubernetes `PersistentVolumeClaim` to another.
This is the original pv-migrate workflow and uses rsync, usually over SSH, with several Kubernetes networking strategies.

For exact flags, see the [CLI reference](cli-reference.md#root-command).

## Strategies

`pv-migrate` can try multiple migration strategies in order and fall back when a strategy is not applicable.

| Name | Description |
| --- | --- |
| `mount` | Mounts both PVCs in a single pod and runs rsync locally, without SSH or networking. Only applicable when source and destination PVCs are in the same namespace and can be mounted by a single pod. |
| `clusterip` | Runs rsync over SSH through a Kubernetes `ClusterIP` Service. Only applicable when source and destination PVCs are in the same cluster. |
| `loadbalancer` | Runs rsync over SSH through a `LoadBalancer` Service. Works across clusters if the load balancer becomes reachable. |
| `nodeport` | Runs rsync over SSH through a `NodePort` Service. Not enabled by default. You can set a specific port with `--helm-set sshd.service.nodePort=<port>`. |
| `local` | Runs sshd on both sides and tunnels traffic through the local machine using Kubernetes port-forwarding and an SSH reverse proxy. Useful for air-gapped or restricted clusters, but recommended only for smaller transfers. |

## Examples

Copy between two PVCs in the same namespace:

```bash
$ pv-migrate --source old-pvc --dest new-pvc
```

Copy between namespaces:

```bash
$ pv-migrate \
  --source-namespace source-ns --source old-pvc \
  --dest-namespace dest-ns --dest new-pvc
```

Copy between different clusters:

```bash
$ pv-migrate \
  --source-kubeconfig /path/to/source/kubeconfig \
  --source-context source-context \
  --source-namespace source-ns \
  --source old-pvc \
  --dest-kubeconfig /path/to/dest/kubeconfig \
  --dest-context dest-context \
  --dest-namespace dest-ns \
  --dest-delete-extraneous-files \
  --dest new-pvc
```

Use custom data mover images:

```bash
$ pv-migrate \
  --helm-set rsync.image.repository=mycustomrepo/rsync \
  --helm-set rsync.image.tag=v1.2.3 \
  --helm-set sshd.image.repository=mycustomrepo/sshd \
  --helm-set sshd.image.tag=v1.2.3 \
  --source old-pvc \
  --dest new-pvc
```

Enable network policies on clusters with deny-all traffic rules:

```bash
$ pv-migrate \
  --helm-set sshd.networkPolicy.enabled=true \
  --helm-set rsync.networkPolicy.enabled=true \
  --source-namespace source-ns --source old-pvc \
  --dest-namespace dest-ns --dest new-pvc
```

Pass additional rsync arguments:

```bash
$ pv-migrate \
  --rsync-extra-args "--partial --inplace" \
  --source old-pvc \
  --dest new-pvc
```

Throttle a migration for manual status/progress testing:

```bash
$ pv-migrate \
  --rsync-extra-args "--bwlimit=1024" \
  --source old-pvc \
  --dest new-pvc
```

Use the NodePort strategy with a specific port:

```bash
$ pv-migrate \
  --strategies nodeport \
  --helm-set sshd.service.nodePort=30555 \
  --source old-pvc \
  --dest new-pvc
```

## Detached Mode

For large migrations, use `--detach` to let the migration continue in the cluster without keeping the CLI connected:

```bash
$ pv-migrate --source old-pvc --dest new-pvc --detach --id my-db-migration
$ pv-migrate status my-db-migration
$ pv-migrate status my-db-migration --follow
$ pv-migrate cleanup my-db-migration
```

`status --follow` shows a live progress bar while the rsync job is running.

## Push Mode

By default, sshd runs on the source side and rsync pulls data from it.
When the source side cannot expose a service, for example behind a firewall or NAT, use `--rsync-push` to reverse the direction:

```bash
$ pv-migrate \
  --source-kubeconfig /path/to/source/kubeconfig \
  --source old-pvc \
  --dest-kubeconfig /path/to/dest/kubeconfig \
  --dest new-pvc \
  --rsync-push
```

`--rsync-push` has no effect for the `mount` and `local` strategies.

## Non-Root Mode

Use `--non-root` on clusters that enforce restricted pod security.
For rsync-based migration this runs containers as a non-root user and skips ownership and directory timestamp preservation.
The migration can fail if source files are not readable by the non-root user or if the destination volume is not writable by it.

For further customization of rendered manifests, see the [Helm chart values](../internal/helm/pv-migrate).
