# Usage

`pv-migrate` supports:

- [PVC-to-PVC migration](migrate.md): copy data directly from one Kubernetes `PersistentVolumeClaim` to another using rsync-based strategies.
- [Bucket backup and restore](backup-restore.md): back up a PVC to object storage, restore it later, or use pv-migrate as the data mover in a scheduled backup `CronJob`.

See [Installation](install.md) for CLI and kubectl plugin setup.

For exact flags and generated command help, see the [CLI reference](cli-reference.md).

The Kubernetes resources created by pv-migrate are sourced from the embedded [Helm chart](../internal/helm/pv-migrate).
You can pass raw values to the backing Helm chart using the `--helm-*` flags for further customization:
container images, resources, service accounts, annotations, labels, affinity, tolerations, and other chart values.

## Detached Operations

Both migration and bucket backup/restore operations support detach mode.
Use `--detach` to let the data mover job continue in the cluster after the CLI exits.

```bash
$ pv-migrate --source old-pvc --dest new-pvc --detach --id my-migration
$ pv-migrate status my-migration
$ pv-migrate status my-migration --follow
$ pv-migrate cleanup my-migration
```

For bucket backup/restore, use the same status and cleanup commands with the operation ID:

```bash
$ pv-migrate backup --source app-data --backend s3 --bucket backups --name app-data --detach --id app-backup
$ pv-migrate status app-backup
$ pv-migrate cleanup app-backup
```

## Where To Go Next

- Start with [PVC-to-PVC migration](migrate.md) if you are moving data between Kubernetes volumes.
- Start with [bucket backup and restore](backup-restore.md) if you want a durable object-storage backup or a later restore.
- Use the [CLI reference](cli-reference.md) when you need exact flag names and defaults.
