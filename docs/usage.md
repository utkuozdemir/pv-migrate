# Usage

`pv-migrate` has two workflows:

- [PVC-to-PVC migration](migrate.md): copy data directly from one Kubernetes `PersistentVolumeClaim` to another, with rsync.
- [Bucket backup and restore](backup-restore.md): back up a PVC to object storage and restore it later, with rclone. This is also the data mover for a scheduled backup `CronJob`.

See [Installation](install.md) for getting the CLI or the kubectl plugin, and the [CLI reference](cli-reference.md) for every flag and the generated help.

## Customizing the generated resources

The Kubernetes resources `pv-migrate` creates come from an embedded [Helm chart](../internal/helm/pv-migrate).
The `--helm-*` flags pass raw values to that chart: container images, resources, service accounts, annotations, labels, affinity, tolerations, and everything else the chart exposes.

## Detached operations

Both migration and bucket backup/restore support detach mode.
With `--detach`, the data mover job keeps running in the cluster after the CLI exits, and you check on it later:

```bash
pv-migrate --source old-pvc --dest new-pvc --detach --id my-migration
pv-migrate status my-migration
pv-migrate status my-migration --follow
pv-migrate cleanup my-migration
```

The same `status` and `cleanup` commands work for backup and restore:

```bash
pv-migrate backup --source app-data --backend s3 --bucket backups --name app-data --detach --id app-backup
pv-migrate status app-backup
pv-migrate cleanup app-backup
```

## Cleanup

After an attached operation completes, `pv-migrate` uninstalls the Helm release it created.
Use `--no-cleanup` or `--no-cleanup-on-failure` when you want to inspect the generated resources.

Detached operations are not cleaned up automatically.
Run `pv-migrate cleanup <id>` once the job is done.

## Where to go next

- [PVC-to-PVC migration](migrate.md) if you are moving data between Kubernetes volumes.
- [Bucket backup and restore](backup-restore.md) if you want a backup in object storage or a later restore.
- [CLI reference](cli-reference.md) for the exact flag names and defaults.
