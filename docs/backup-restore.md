# Bucket backup and restore

Bucket backup/restore copies PVC data to object storage and restores it later.
It uses rclone inside a Kubernetes Job and supports S3-compatible storage, Azure Blob, GCS, and raw rclone config mode.

See the [CLI reference](cli-reference.md#backup) for all flags.

## Managed bucket mode

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

By default, restore copies backup contents into the destination and keeps
destination files that are not present in the backup. To make the destination an
exact mirror of the backup, opt in to deletion:

```bash
$ pv-migrate restore \
  --dest app-data-restore \
  --backend s3 \
  --bucket pv-backups \
  --name app-data-2026-04-11 \
  --delete-extraneous-files
```

### Credentials

You can pass credentials as flags or environment variables. Explicit flags take
precedence over environment variables.

Prefer environment variables in automated or shared environments so secrets do
not appear in process arguments.

Supported environment variables:

- S3: `PV_MIGRATE_S3_ACCESS_KEY`, `PV_MIGRATE_S3_SECRET_KEY`
- Azure: `PV_MIGRATE_AZURE_STORAGE_ACCOUNT`, `PV_MIGRATE_AZURE_STORAGE_KEY`
- GCS: `PV_MIGRATE_GCS_SERVICE_ACCOUNT_JSON`

For GCS, `PV_MIGRATE_GCS_SERVICE_ACCOUNT_JSON` must contain the JSON credentials
contents. Use `--gcs-service-account-file` when you want to pass a local file
path instead.

Managed S3 mode uses rclone's generic `Other` provider by default. Leave it
unless your provider needs another rclone mode; then set `--s3-provider`.

Managed GCS mode defaults to `bucket_policy_only = true`. Set
`--gcs-bucket-policy-only=false` for legacy buckets that still use object ACLs.

`--name` identifies the backup inside the bucket.
The default prefix is `pv-migrate`, and prefixes can contain `/` for nesting:

```bash
$ pv-migrate backup \
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

The metadata records the backup time and source PVC. It is useful for inspection,
but restore does not need it.

For example:

```text
pv-backups/pv-migrate/app-data-2026-04-11/
pv-backups/pv-migrate/app-data-2026-04-11.meta.yaml
```

## Raw rclone config mode

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

Managed mode tries to write a metadata sidecar file after the data upload
succeeds. Raw rclone config mode does not write that file because pv-migrate
treats the remote spec as an opaque rclone path.

## Subdirectory backup and restore

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

## Detached mode and progress

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
When `--dry-run`, `--dry-run=true`, or `-n` is present in `--rclone-extra-args`,
pv-migrate skips writing the metadata sidecar so the backup run does not mutate the bucket.

## Permissions and ownership

Bucket backup/restore copies file contents. It does not preserve POSIX owner,
group, or mode. Restored files are created by the rclone process user, and
regular files commonly restore with default file permissions such as `0644`.

Use PVC-to-PVC migration if you need owners, groups, or modes to survive the copy.

## Scheduled backups

You can run `pv-migrate backup` from a Kubernetes `CronJob` to create scheduled PVC backups.
This gives you a Kubernetes-native data mover that writes to object storage.

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

- Replace `<version>` with the release tag you want to run. The official pv-migrate image has no shell, so use direct `args` as shown.
- The example uses the `Job` name created by the CronJob as `--name`, so each run writes to a distinct backup prefix.
- pv-migrate does not handle retention or make app-consistent backups. Use bucket lifecycle policies for retention, and pause or snapshot workloads that need transactional consistency.

## Non-root mode

`backup` and `restore` support `--non-root`.
This runs the rclone container as UID/GID `10000` and sets `fsGroup` to `10000`.

This can help in restricted PodSecurity clusters, but it has the normal non-root filesystem constraints:

- Backup can fail if files are not readable by UID/GID `10000`.
- Restore can fail if the destination volume is not writable by UID/GID `10000` or if the CSI driver does not honor `fsGroup`.

For further customization of rendered manifests, see the [Helm chart values](../internal/helm/pv-migrate).
