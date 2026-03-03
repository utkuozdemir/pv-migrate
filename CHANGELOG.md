# Changelog

## v2.9.9-bedag (2026-03-03)

### Added

- **Batch migration mode**
 When used with the `lbsvc`/`loadbalancer` strategy, all PVCs within
  the same source namespace are mounted into a **single** sshd pod behind a
  **single** LoadBalancer service. Rsync jobs then run sequentially against that
  shared endpoint.

- **`lbsvc` strategy alias**
  `lbsvc` is now accepted as an alias for `loadbalancer`, preserving
  compatibility with existing wrapper scripts.

### Known Bugs

- **Spurious "Failed to watch" log during cleanup**

This occurrs during the cleanup phase and is a harmless warning.

---

### Why this matters for environments with many namespaces and 0–10 PVCs each

In a typical migration run across dozens of namespaces where each namespace
holds a handful of PVCs, the previous behaviour was:

| Step | Old behaviour (per PVC) | New behaviour (per namespace) |
|------|------------------------|-------------------------------|
| Create sshd pod | 1 per PVC | **1 per namespace** |
| Create LoadBalancer service | 1 per PVC | **1 per namespace** |
| Wait for LB external IP | 1 wait per PVC (often 30–90 s each) | **1 wait per namespace** |
| Generate SSH key pair | 1 per PVC | **1 per namespace** |
| Helm install (source side) | 1 per PVC | **1 per namespace** |
| Helm uninstall (source side) | 1 per PVC | **1 per namespace** |

**Concrete impact for a namespace with 5 PVCs:**

- **Before:** 5 sshd pods, 5 LoadBalancer services, 5 LB IP waits, 5 Helm
  install/uninstall cycles on the source side. Each LB IP allocation can take
  30–90 seconds depending on the cloud provider, so the overhead alone is
  2.5–7.5 minutes just waiting for IPs — on top of the actual data transfer.

- **After:** 1 sshd pod (mounting all 5 PVCs), 1 LoadBalancer service, 1 LB IP
  wait, 1 Helm install, and 1 Helm uninstall on the source side. The 5 rsync
  jobs run sequentially against the same endpoint. Total LB overhead drops from
  minutes to a single 30–90 second wait.

**Across 20 namespaces × ~5 PVCs each (100 transfers):**

| Metric | Before | After | Reduction |
|--------|--------|-------|-----------|
| LoadBalancer services created | 100 | 20 | **80%** |
| LB IP wait time (@ 60 s avg) | ~100 min | ~20 min | **80 min saved** |
| Helm source installs | 100 | 20 | **80%** |
| sshd pods | 100 | 20 | **80%** |
| Cloud LB resources consumed concurrently | 1 per PVC | 1 per namespace | **80% fewer** |

Additional benefits:

- **Reduced cloud provider API pressure.** Fewer LoadBalancer allocations means
  fewer calls to the cloud LB controller, which matters in environments with
  strict API rate limits.
- **Lower risk of IP exhaustion.** Some environments have a limited pool of
  external IPs; creating 1 LB per namespace instead of 1 per PVC keeps well
  within those limits.
- **Simpler cleanup.** Fewer Helm releases to track and uninstall means less
  chance of orphaned resources if a run is interrupted.
- **Single YAML file input.** No need for a wrapper script to loop over PVC
  pairs — define them once in a transfers file and let pv-migrate handle
  grouping and sequencing.
