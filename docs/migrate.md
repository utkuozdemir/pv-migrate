# PVC-to-PVC migration

PVC-to-PVC migration copies data directly from one Kubernetes `PersistentVolumeClaim` to another.
It is the original `pv-migrate` workflow.
The copy is done by rsync, usually over SSH, and the strategies below differ in how the two sides reach each other.

See the [CLI reference](cli-reference.md#root-command) for all flags.

## Strategies

`pv-migrate` tries the strategies in order and moves on to the next one when a strategy does not apply or fails.
The migration succeeds with the first strategy that works.
The default order is cheapest first, and `--strategies` overrides it.

| Name | Description |
| --- | --- |
| `mount` | Mounts both PVCs in a single pod and runs rsync locally, without SSH or networking. Only applies when both PVCs are in the same namespace and can be mounted by one pod. |
| `clusterip` | rsync over SSH through a `ClusterIP` Service. Only applies when both PVCs are in the same cluster. |
| `loadbalancer` | rsync over SSH through a `LoadBalancer` Service. Works across clusters when the load balancer address is reachable from the other side. |
| `nodeport` | rsync over SSH through a `NodePort` Service. Opt-in, because it depends on the nodes being reachable. The port can be fixed with `--helm-set sshd.service.nodePort=<port>`. |
| `local` | Runs sshd on both sides and tunnels the traffic through the machine running the CLI, with Kubernetes port-forwarding and an SSH reverse tunnel. Opt-in, for restricted clusters, and recommended for smaller transfers only, since all data goes through your machine. |

## Examples

Copy between two PVCs in the same namespace:

```bash
pv-migrate --source old-pvc --dest new-pvc
```

Copy between namespaces:

```bash
pv-migrate \
  --source-namespace source-ns --source old-pvc \
  --dest-namespace dest-ns --dest new-pvc
```

Copy between clusters:

```bash
pv-migrate \
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

If files are deleted from the source while the transfer is running, rsync skips them.
The migration still succeeds, and a warning line names the condition.
With `--dest-delete-extraneous-files`, the skipped files keep their existing copies on the destination.
Re-run the migration in that case, or copy from a source that is not being written to.

Use custom data mover images:

```bash
pv-migrate \
  --helm-set rsync.image.repository=mycustomrepo/rsync \
  --helm-set rsync.image.tag=v1.2.3 \
  --helm-set sshd.image.repository=mycustomrepo/sshd \
  --helm-set sshd.image.tag=v1.2.3 \
  --source old-pvc \
  --dest new-pvc
```

Enable network policies on clusters with deny-all traffic rules:

```bash
pv-migrate \
  --helm-set sshd.networkPolicy.enabled=true \
  --helm-set rsync.networkPolicy.enabled=true \
  --source-namespace source-ns --source old-pvc \
  --dest-namespace dest-ns --dest new-pvc
```

Pass additional rsync arguments:

```bash
pv-migrate \
  --rsync-extra-args "--partial --inplace" \
  --source old-pvc \
  --dest new-pvc
```

Throttle a migration, e.g., to try out `status` and the progress output:

```bash
pv-migrate \
  --rsync-extra-args "--bwlimit=1024" \
  --source old-pvc \
  --dest new-pvc
```

Use the `nodeport` strategy with a fixed port:

```bash
pv-migrate \
  --strategies nodeport \
  --helm-set sshd.service.nodePort=30555 \
  --source old-pvc \
  --dest new-pvc
```

## Detached mode

For large migrations, `--detach` lets the job continue in the cluster after the CLI exits:

```bash
pv-migrate --source old-pvc --dest new-pvc --detach --id my-db-migration
pv-migrate status my-db-migration
pv-migrate status my-db-migration --follow
pv-migrate cleanup my-db-migration
```

`status --follow` shows a live progress bar while the rsync job is running.

## Push mode

By default, sshd runs on the source side and rsync pulls the data from it.
When the source side cannot expose a service, e.g., behind a firewall or NAT, `--rsync-push` reverses the direction:

```bash
pv-migrate \
  --source-kubeconfig /path/to/source/kubeconfig \
  --source old-pvc \
  --dest-kubeconfig /path/to/dest/kubeconfig \
  --dest new-pvc \
  --rsync-push
```

`--rsync-push` has no effect on the `mount` and `local` strategies.

## Non-root mode

Use `--non-root` on clusters that enforce restricted pod security.
The containers then run as a non-root user, and rsync skips preserving ownership and directory timestamps.

The migration fails if the non-root user cannot read the source files or cannot write to the destination volume.

For further customization of the generated manifests, see the [Helm chart values](../internal/helm/pv-migrate).
