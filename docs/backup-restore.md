# Bucket backup and restore

Bucket backup and restore copies PVC data to object storage and restores it later.
An rclone job inside the cluster does the copy.
The built-in backends are S3-compatible storage, Azure Blob and GCS, and a raw rclone config covers everything else rclone can reach.

See the [CLI reference](cli-reference.md#backup) for all flags.

## Managed bucket mode

In managed mode, `pv-migrate` builds the rclone config from its own flags.
Use it when your backend is one of the built-in ones.

S3-compatible backup:

```bash
pv-migrate backup \
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
pv-migrate restore \
  --dest app-data-restore \
  --dest-namespace app \
  --backend s3 \
  --bucket pv-backups \
  --endpoint https://s3.example.com \
  --access-key "$ACCESS_KEY" \
  --secret-key "$SECRET_KEY" \
  --name app-data-2026-04-11
```

By default, restore copies the backup into the destination and keeps the destination files that are not in the backup.
To make the destination an exact mirror of the backup, opt in to deletion:

```bash
pv-migrate restore \
  --dest app-data-restore \
  --backend s3 \
  --bucket pv-backups \
  --name app-data-2026-04-11 \
  --delete-extraneous-files
```

### Credentials

Credentials can be passed as flags or as environment variables.
A flag wins over the environment variable.

Prefer the environment variables in automation and on shared machines, so that the credentials do not show up in the process list.

Supported environment variables:

- S3: `PV_MIGRATE_S3_ACCESS_KEY`, `PV_MIGRATE_S3_SECRET_KEY`
- Azure: `PV_MIGRATE_AZURE_STORAGE_ACCOUNT`, `PV_MIGRATE_AZURE_STORAGE_KEY`
- GCS: `PV_MIGRATE_GCS_SERVICE_ACCOUNT_JSON`

For GCS, `PV_MIGRATE_GCS_SERVICE_ACCOUNT_JSON` holds the JSON credentials themselves, not a path.
Use `--gcs-service-account-file` to pass a local file instead.

Managed S3 mode uses rclone's generic `Other` provider by default.
Leave it unless your provider needs another rclone mode, and set `--s3-provider` in that case.

Managed GCS mode defaults to `bucket_policy_only = true`.
Set `--gcs-bucket-policy-only=false` for legacy buckets that still use object ACLs.

`--name` identifies the backup inside the bucket.
The default prefix is `pv-migrate`, and prefixes can contain `/` for nesting:

```bash
pv-migrate backup \
  --source app-data \
  --backend s3 \
  --bucket pv-backups \
  --prefix teams/payments/prod \
  --name app-data-2026-04-11
```

## Object layout

In managed mode, backup data is stored under:

```text
<bucket>/<prefix>/<name>/
```

The backup metadata sidecar is stored at:

```text
<bucket>/<prefix>/<name>.meta.yaml
```

The metadata records the backup time and the source PVC.
It is there for inspection, restore does not need it.

For example:

```text
pv-backups/pv-migrate/app-data-2026-04-11/
pv-backups/pv-migrate/app-data-2026-04-11.meta.yaml
```

## Raw rclone config mode

Use raw rclone config mode when you need a backend or an rclone option that `pv-migrate` does not model.
In this mode, `--remote` is the full source or destination path, and `--name`, `--bucket` and `--prefix` are not used to build it.

```bash
pv-migrate backup \
  --source app-data \
  --rclone-config ./rclone.conf \
  --remote myremote:bucket/custom/path
```

Restore with the same raw remote:

```bash
pv-migrate restore \
  --dest app-data-restore \
  --rclone-config ./rclone.conf \
  --remote myremote:bucket/custom/path
```

Managed mode writes the metadata sidecar after the data upload succeeds.
Raw rclone config mode does not, because `pv-migrate` treats the remote as an opaque rclone path.

## Subdirectory backup and restore

Use `--path` to back up or restore a subdirectory inside the PVC:

```bash
pv-migrate backup \
  --source app-data \
  --path uploads \
  --backend s3 \
  --bucket pv-backups \
  --name uploads-2026-04-11
```

The same flag restores into a subdirectory on the target PVC:

```bash
pv-migrate restore \
  --dest app-data-restore \
  --path uploads \
  --backend s3 \
  --bucket pv-backups \
  --name uploads-2026-04-11
```

## Detached mode and progress

Use `--detach` for long backup or restore jobs:

```bash
pv-migrate backup \
  --source app-data \
  --backend s3 \
  --bucket pv-backups \
  --name app-data-2026-04-11 \
  --detach \
  --id app-backup

pv-migrate status app-backup
pv-migrate status app-backup --follow
pv-migrate cleanup app-backup
```

Attached backup and restore runs and `status --follow` read rclone's JSON stats output to show the progress.
To try that out, throttle rclone with `--rclone-extra-args`:

```bash
pv-migrate backup \
  --source app-data \
  --backend s3 \
  --bucket pv-backups \
  --name slow-test \
  --detach \
  --id slow-test \
  --rclone-extra-args '--bwlimit 1M --transfers 1'
```

`--rclone-extra-args` is appended after the rclone flags `pv-migrate` sets itself.
It is for the rclone options that have no flag of their own.
Overriding the built-in stats or JSON log flags breaks the progress parsing.

When `--dry-run`, `--dry-run=true` or `-n` is among the extra arguments, `pv-migrate` also skips writing the metadata sidecar, so a dry run does not change the bucket.

## Permissions and ownership

Bucket backup and restore copies file contents only.
It does not preserve the POSIX owner, group or mode.
Restored files belong to the user the rclone process runs as, and regular files come back with default permissions such as `0644`.

Use PVC-to-PVC migration if the owners, groups or modes have to survive the copy.

## Scheduled backups

`pv-migrate backup` can run from a Kubernetes `CronJob`, which gives you scheduled PVC backups to object storage with Kubernetes building blocks only.

> [!WARNING]
> This is not a backup platform. `pv-migrate` does not manage retention, backup catalogs, restore checks, alerting, encryption or application consistency.
> Pause or snapshot the application before the backup if it needs a consistent copy.
> Use bucket lifecycle rules for retention, and your own monitoring.

The example below runs a nightly S3-compatible backup.
It uses the name of the `Job` the CronJob creates as the backup name, so every run writes to its own prefix.

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
              image: docker.io/utkuozdemir/pv-migrate:<version>
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

Notes:

- Replace `<version>` with the release tag you want to run. The image has no shell, so pass the arguments as `args`, as shown.
- The `Job` name is passed as `--name`, so each run writes to its own backup prefix.
- Retention and application consistency are not handled. Use bucket lifecycle policies for retention, and pause or snapshot workloads that need a consistent copy.

## Non-root mode

`backup` and `restore` support `--non-root`.
The rclone container then runs as UID/GID `10000`, with `fsGroup` set to `10000`.

Use it on clusters that enforce restricted pod security.
It comes with the usual non-root filesystem constraints:

- Backup can fail if files are not readable by UID/GID `10000`.
- Restore can fail if the destination volume is not writable by UID/GID `10000` or if the CSI driver does not honor `fsGroup`.

For further customization of the generated manifests, see the [Helm chart values](../internal/helm/pv-migrate).
