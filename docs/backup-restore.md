# Bucket Backup And Restore

Bucket backup/restore copies PVC data to object storage and restores it later.
It uses rclone inside a Kubernetes Job and supports S3-compatible storage, Azure Blob, GCS, and raw rclone config mode.

For exact flags, see the [CLI reference](cli-reference.md#backup).

## Managed Bucket Mode

Managed mode builds the rclone config from pv-migrate flags.
Use this when your backend is one of the built-in backend types.

S3-compatible backup:

```bash
$ pv-migrate backup \
  --source app-data \
  --source-namespace app \
  --backend s3 \
  --bucket pv-backups \
  --endpoint https://s3.example.com \
  --access-key "$ACCESS_KEY" \
  --secret-key "$SECRET_KEY" \
  --name app-data-2026-04-11
```

Restore from that backup:

```bash
$ pv-migrate restore \
  --dest app-data-restore \
  --dest-namespace app \
  --backend s3 \
  --bucket pv-backups \
  --endpoint https://s3.example.com \
  --access-key "$ACCESS_KEY" \
  --secret-key "$SECRET_KEY" \
  --name app-data-2026-04-11
```

Credential flags can also be provided through environment variables:
`PV_MIGRATE_S3_ACCESS_KEY`, `PV_MIGRATE_S3_SECRET_KEY`,
`PV_MIGRATE_AZURE_STORAGE_ACCOUNT`, `PV_MIGRATE_AZURE_STORAGE_KEY`, and
`PV_MIGRATE_GCS_SERVICE_ACCOUNT_JSON`.
Explicit flags take precedence over environment variables.
Prefer environment variables in automated and shared environments to avoid exposing secrets in process arguments.

The backup name is the logical identity of the backup in the bucket.
The default prefix is `pv-migrate`, and prefixes can contain `/` for nesting:

```bash
$ pv-migrate backup \
  --source app-data \
  --backend s3 \
  --bucket pv-backups \
  --prefix teams/payments/prod \
  --name app-data-2026-04-11
```

## Object Layout

In managed mode, backup data is stored under:

```text
<bucket>/<prefix>/<name>/
```

The backup metadata sidecar is stored at:

```text
<bucket>/<prefix>/<name>.meta.yaml
```

For example:

```text
pv-backups/pv-migrate/app-data-2026-04-11/
pv-backups/pv-migrate/app-data-2026-04-11.meta.yaml
```

## Raw Rclone Config Mode

Use raw rclone config mode when you need a backend or rclone option that pv-migrate does not model directly.
In this mode, `--remote` controls the destination/source path and `--name`, `--bucket`, and `--prefix` are not used for path construction.

```bash
$ pv-migrate backup \
  --source app-data \
  --rclone-config ./rclone.conf \
  --remote myremote:bucket/custom/path
```

Restore with the same raw remote:

```bash
$ pv-migrate restore \
  --dest app-data-restore \
  --rclone-config ./rclone.conf \
  --remote myremote:bucket/custom/path
```

Managed-mode backups write a metadata sidecar file. Raw rclone config mode does not write that sidecar,
because pv-migrate treats the remote spec as an opaque rclone path.

## Subdirectory Backup And Restore

Use `--path` to back up or restore a subdirectory inside the PVC:

```bash
$ pv-migrate backup \
  --source app-data \
  --path uploads \
  --backend s3 \
  --bucket pv-backups \
  --name uploads-2026-04-11
```

The same flag restores into a subdirectory on the target PVC:

```bash
$ pv-migrate restore \
  --dest app-data-restore \
  --path uploads \
  --backend s3 \
  --bucket pv-backups \
  --name uploads-2026-04-11
```

## Detached Mode And Progress

Use `--detach` for long backup or restore jobs:

```bash
$ pv-migrate backup \
  --source app-data \
  --backend s3 \
  --bucket pv-backups \
  --name app-data-2026-04-11 \
  --detach \
  --id app-backup

$ pv-migrate status app-backup
$ pv-migrate status app-backup --follow
$ pv-migrate cleanup app-backup
```

Attached backup/restore and `status --follow` use rclone's JSON stats output to render progress.
For manual testing, throttle rclone with `--rclone-extra-args`:

```bash
$ pv-migrate backup \
  --source app-data \
  --backend s3 \
  --bucket pv-backups \
  --name slow-test \
  --detach \
  --id slow-test \
  --rclone-extra-args '--bwlimit 1M --transfers 1'
```

`--rclone-extra-args` is appended after pv-migrate's built-in rclone progress flags.
It is useful as an escape hatch, but overriding the built-in stats or JSON log flags can break progress parsing.

## Scheduled Backups

You can run `pv-migrate backup` from a Kubernetes `CronJob` to create scheduled PVC backups.
This is useful when you want a simple Kubernetes-native data mover that writes to object storage.

> [!WARNING]
> This is not a full backup platform. pv-migrate does not manage retention, backup catalogs, restore verification,
> alerting, encryption policy, or transactional consistency. Pause your application before backup if needed.
> Use bucket lifecycle rules, separate cleanup automation, and monitoring where needed.

The example below runs a nightly S3-compatible backup.
It uses the Kubernetes `Job` name in the backup name so each scheduled run writes to a distinct object prefix.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: pv-migrate-backup-s3
  namespace: app
type: Opaque
stringData:
  access-key: replace-me
  secret-key: replace-me
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: pv-migrate-backup
  namespace: app
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: pv-migrate-backup
  namespace: app
rules:
  - apiGroups: [""]
    resources: ["persistentvolumeclaims"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["pods", "pods/log"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["secrets", "serviceaccounts"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: pv-migrate-backup
  namespace: app
subjects:
  - kind: ServiceAccount
    name: pv-migrate-backup
    namespace: app
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: pv-migrate-backup
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: app-data-backup
  namespace: app
spec:
  schedule: "0 2 * * *"
  concurrencyPolicy: Forbid
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 3
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: pv-migrate-backup
          restartPolicy: Never
          containers:
            - name: pv-migrate
              image: docker.io/utkuozdemir/pv-migrate:latest
              env:
                - name: PV_MIGRATE_S3_ACCESS_KEY
                  valueFrom:
                    secretKeyRef:
                      name: pv-migrate-backup-s3
                      key: access-key
                - name: PV_MIGRATE_S3_SECRET_KEY
                  valueFrom:
                    secretKeyRef:
                      name: pv-migrate-backup-s3
                      key: secret-key
                - name: BACKUP_NAME
                  valueFrom:
                    fieldRef:
                      fieldPath: metadata.labels['batch.kubernetes.io/job-name']
              args:
                - backup
                - --source=app-data
                - --source-namespace=app
                - --ignore-mounted
                - --backend=s3
                - --bucket=pv-backups
                - --endpoint=https://s3.example.com
                - --prefix=scheduled/app
                - --name=$(BACKUP_NAME)
```

Notes for scheduled backups:

- The official pv-migrate image is a scratch image and does not include a shell, so use direct `args` as shown.
- This example uses Kubernetes argument expansion and the CronJob-created `Job` name as the backup name.
- If you set a fixed `--name`, repeated runs write to the same object prefix unless bucket versioning or another retention mechanism handles that.
- If you need timestamped names or custom pre/post scripts, run pv-migrate from your own wrapper image or automation that provides those tools.
- Use object-storage lifecycle policies or a separate cleanup job for retention.
- Store object-storage credentials in Kubernetes Secrets, not inline in the CronJob manifest.
- Prefer environment variables for credentials in Pod manifests, so secret values are not exposed in process arguments.
- Decide whether `--ignore-mounted` is safe for the workload. For databases and other stateful systems, pause the app, use an application dump, or take a storage snapshot first if you need transactional consistency.
- Ensure the CronJob service account can create, patch, watch, and delete the Kubernetes resources pv-migrate manages through Helm. You may need to extend the RBAC example if you use Helm overrides or a custom environment.
- Remember that bucket backup/restore does not currently preserve POSIX owner, group, or mode.

## Non-Root Mode

`backup` and `restore` support `--non-root`.
This runs the rclone container as UID/GID `10000` and sets `fsGroup` to `10000`.

This can help in restricted PodSecurity clusters, but it has the normal non-root filesystem constraints:

- Backup can fail if files are not readable by UID/GID `10000`.
- Restore can fail if the destination volume is not writable by UID/GID `10000` or if the CSI driver does not honor `fsGroup`.

## Permissions And Ownership

Bucket backup/restore is content-oriented and does not currently preserve POSIX owner, group, or mode across a backup/restore round trip.
Restored files are created by the rclone process user, and regular files commonly restore with default file permissions such as `0644`.

Use PVC-to-PVC migration when POSIX metadata fidelity is required.

## Cleanup

By default, pv-migrate cleans up the Helm release after attached operations complete.
Use `--no-cleanup` or `--no-cleanup-on-failure` when you need to inspect generated resources.

Detached operations are not cleaned up automatically because the CLI exits after the job starts.
Use `pv-migrate cleanup <id>` after the job completes.

For further customization of rendered manifests, see the [Helm chart values](../internal/helm/pv-migrate).
