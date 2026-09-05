# Security model

This page says what you can expect from `pv-migrate` security-wise, where the trust boundaries are, and how the project checks that it holds.
For reporting a problem, see [SECURITY.md](../.github/SECURITY.md).

## What the tool does

`pv-migrate` is a one-shot client that owns no state.
It generates the manifests from an embedded Helm chart, installs them as a release, waits for the data mover job, reads its log, and uninstalls the release.
The data itself is moved by rsync or rclone, running inside the cluster.

- It needs a kubeconfig with rights to create and delete the resources of that release in the namespaces involved. Those are a Job, a Deployment and a Service for the SSH side, Secrets, ServiceAccounts and NetworkPolicies. The RBAC example in [Bucket backup and restore](backup-restore.md#scheduled-backups) lists them. The NetworkPolicies are the one optional item: without the right to create them, the tool leaves them out, see below.
- Nothing is copied through the machine running the CLI, except with the opt-in `local` strategy, which exists for clusters that cannot reach each other.
- On the wire, PVC-to-PVC transfers are rsync over SSH. A fresh Ed25519 (or, with `--ssh-key-algorithm rsa`, RSA 2048) key pair is generated for every run and lives only in the release's Secrets. It is removed with the release.
- Bucket credentials reach the cluster as a Kubernetes Secret and the rclone process as environment variables, never on a command line. The connection to the bucket is TLS unless you point `--endpoint` at a plain `http://` URL.
- The `mount` strategy uses no network at all: both PVCs are mounted into one pod.

## What you can expect

- The data mover containers run as root by default, because rsync needs that to preserve ownership, and the sshd container additionally gets the `SYS_CHROOT` capability in that mode. `--non-root` runs them as a dedicated user (UID 10000) without that capability, and gives up ownership and directory timestamp preservation, see [Non-root mode](migrate.md#non-root-mode).
- The sshd side accepts public key authentication only, for the one key generated for the run. Password and challenge-response authentication are off.
- Each component that uses the network gets an allow-all network policy that selects only its own pod. In a cluster without other policies that is a no-op, and in a namespace isolated with Kubernetes network policies it is what lets the SSH connection establish at all. A deny rule in a CNI's own policy type, e.g., a Cilium or Calico deny, still applies to these pods as to any other. When the account is not allowed to create network policies, the tool checks that first and leaves them out with a warning instead of failing the install. Chart values turn them off per component.
- User input that ends up in the data mover command is quoted as a single shell word. The rsync and rclone commands are assembled as shell strings inside the chart, so a path with a space or a quote in it stays one argument. A value with a character that cannot be carried on one YAML line is rejected with an error naming the flag, instead of breaking the chart.
- `--rsync-extra-args` and `--rclone-extra-args` are the deliberate exception. They are appended to the command unquoted, as documented, and whoever runs the CLI already controls the whole command anyway.
- `--source-path` and `--dest-path` are resolved against the volume's mount point and must stay inside it. A path like `../` would otherwise name the container's root, which rsync would write to and, with the delete flag, delete from. The check is lexical, so a symlink that already exists inside the volume can still point outside of it.
- The release artifacts are signed keyless with Sigstore, and every archive and image carries a GitHub build provenance attestation. Release tags are signed. Before it builds anything, the release workflow checks that GitHub verifies the tag's signature, that the signed tag names the release and points at the commit being built, and that the commit is on the main branch. Release tags cannot be moved or deleted. The auto-generated GitHub source archives are not covered. See [Verifying what you downloaded](install.md#verifying-what-you-downloaded).

## Trust boundaries

1. The clusters. The Kubernetes API servers and whatever runs on the nodes are trusted. `pv-migrate` cannot protect the data from a cluster administrator, and does not try to.
2. The data. The contents of the volumes are opaque to the tool. rsync and rclone copy bytes, and neither one interprets them.
3. The flags. Whoever runs the CLI controls the command, including the raw extra arguments. The flags are trusted input, and the quoting exists so that an ordinary path with a space works, not to keep an operator from running a command.
4. The data mover output. The job logs are parsed for progress and for the exit code. That text comes from rsync, rclone or a custom image, so the parsers are fuzzed and a bad line is refused rather than repaired where it would fabricate a result.
5. The supply chain. Releases are built by the release workflow from a signed tag. Actions are pinned by commit and base images by digest. The checksums file and the images are signed in that workflow, tied to its identity.

## Threats and what is done about them

| Threat | What is done |
| --- | --- |
| A path or spec breaks out of the data mover command | Every interpolated value is quoted as one shell word, and values YAML cannot carry are rejected at the flag. A table test runs the quoter's output through a real shell. |
| A user path escapes the volume | `--source-path` and `--dest-path` are resolved against the mount point and rejected if they leave it, before anything touches the cluster. The backup and restore `--path` goes through the same kind of check, and that one is fuzzed. |
| Someone else connects to the transfer's sshd | Key authentication only, for a key generated for this run and thrown away with it. Network policies can restrict the peers further. |
| Credentials leak through the process list or the logs | Bucket credentials travel in a Secret and reach rclone through the environment. They can be given to the CLI through environment variables too, see [Credentials](backup-restore.md#credentials). |
| Malformed data mover output confuses the progress or the result | Both progress parsers guarantee a percentage within range and a total not below the transferred count, and refuse lines that would need fabricating a number. The exit code is read from the job, not inferred from the log. |
| Tampered release artifact | Keyless signatures on the checksums file and the images, build provenance attestations, reproducible archives that a second runner rebuilds and compares on every release. |
| Compromised dependency or action | Renovate updates, Dependabot alerts, actions pinned by commit, images pinned by digest, gosec on every change. |

## Out of scope

- Protecting the data from the cluster administrators, or from anyone with the kubeconfig you run the tool with.
- Encryption at rest in the bucket beyond what the storage provider does. Configure it on the bucket.
- Application consistency. `pv-migrate` copies files, it does not quiesce databases. Pause or snapshot the workload if the copy has to be consistent.
- The data mover images between releases. They are rebuilt only when a release is cut, so a fix in Alpine's rsync, OpenSSH or rclone packages reaches the published images with the next release.

## How this is checked

- The progress parsers, the rclone config generation and the bucket path checks: fuzz targets, run with `task test:fuzz`. The seed corpora are replayed by every `task test` run. The migration path check has a table test.
- The shell quoting: a table test that runs the quoted strings through a real shell, since a hand-written splitter could share a blind spot with the quoter.
- The chart's job scripts: the chart is generated in a Go test and the scripts run under a real shell with a stand-in data mover.
- The end-to-end behavior: the integration suites run real migrations across two kind clusters with Cilium in default-deny mode and the network policies enabled, and real backups against MinIO, on every change.
- The code: golangci-lint with gosec on every pull request and push to the main branch, plus the race detector in the migration suite.
- The supply chain: OpenSSF Scorecard scores the repository weekly, from the outside. The README badge links the current score.
- The releases: the release workflow rebuilds and compares the archives itself. The signature and attestation commands in the install guide are run manually after a release.
