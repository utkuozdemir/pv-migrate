---
name: Bug report
about: Something does not work as documented
title: ''
labels: ''
assignees: ''

---

**What happens**

A short description of the behavior.

**How to reproduce**

1. Run `pv-migrate ...`
2. ...

**What should happen instead**

**Output**

The command output, ideally with `--log-level DEBUG`. Redact anything you do not want public.

**Environment**

- `pv-migrate` version (`pv-migrate --version`), and your OS and architecture, e.g., macOS arm64
- How it was installed, e.g., Homebrew, krew, release archive
- Source and destination Kubernetes versions, e.g., `v1.31.4-gke.1183000`, `v1.32.1+k3s1`
- Source and destination PVC storage class, access modes and size, e.g., `gce-pd ReadWriteOnce 8Gi -> local-path ReadWriteOnce 8Gi`
- For bucket backup/restore: the backend, e.g., S3-compatible (which provider), Azure Blob, GCS, raw rclone config

**Anything else**

Network policies, restricted pod security, a proxy, anything unusual about the clusters.
