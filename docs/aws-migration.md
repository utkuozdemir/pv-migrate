# AWS PVC migration

Use the `loadbalancer` strategy to copy PVC contents between EKS clusters when the rsync pod can reach a TCP SSH endpoint exposed by the other cluster.
This guide adapts the operational experience in [issue #257](https://github.com/utkuozdemir/pv-migrate/issues/257) to the current CLI.
The annotations below describe an AWS Load Balancer Controller configuration for an NLB with instance targets; they are not a universal configuration for every EKS mode or load balancer controller.
Check the annotations supported by the controller managing Services in your cluster before using them.

## Prepare the volumes and network

Create the destination PVC with sufficient capacity and ensure both clusters can schedule pods that mount their respective volumes.
Quiesce applications writing to either volume, using the application's shutdown or backup procedure, and stop the pods mounting the claims before copying.
A file copy of a running database is not necessarily a consistent backup.

By default, rsync runs in the destination cluster and initiates SSH connections to sshd in the source cluster, where the LoadBalancer Service is created.
With `--rsync-push`, rsync runs at the source and connects to sshd and its Service at the destination; the data still moves from source to destination.
Choose the exposure and network rules for the side hosting sshd.

The example requests an internet-facing NLB.
The `external` annotation selects controller handling; it does not itself mean public exposure.
Use an internal scheme instead when your controller supports it and the rsync side has private routing to that load balancer.
Controller configuration, subnet selection, and connectivity determine whether an internal or internet-facing endpoint is usable.
Allow the required TCP SSH traffic from the rsync side to the load balancer's Service port (22 by default), and allow load balancer health checks and forwarding to its targets.
For instance targets, account for the Service's node port on the target nodes.
Check the relevant security groups, network ACLs, routes, and pod egress rules, including DNS access, rather than disabling isolation broadly.

The chart creates allow-all Kubernetes network policies selecting its own networked pods by default.
If the account cannot create those policies, pv-migrate skips them with a warning, and existing isolation may require explicit allowances.
CNI-specific deny rules still apply, and these policies do not change AWS network controls.
See [PVC-to-PVC migration](migrate.md) for network-policy customization.

## Configure and run

Save the following as `aws-values.yaml`:

```yaml
sshd:
  service:
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-type: "external"
      service.beta.kubernetes.io/aws-load-balancer-nlb-target-type: "instance"
      service.beta.kubernetes.io/aws-load-balancer-scheme: "internet-facing"
rsync:
  maxRetries: 20
  retryPeriodSeconds: 60
```

Annotation values are strings, while the retry settings are integers.
Using a values file preserves the dots in annotation keys without shell or Helm `--set` escaping.
The retry settings allow one initial rsync attempt and up to 20 retries, with 60 seconds between retryable failures, giving DNS time to become resolvable after a hostname is published.
They are not a guaranteed wall-clock migration deadline: each attempt takes time, and some exit conditions end the job without retrying.

Replace the context, namespace, and PVC names below with your own.
Both contexts must be available in your kubeconfig; use `--source-kubeconfig` and `--dest-kubeconfig` if they are in separate files.

```bash
pv-migrate \
  --source-context source-eks \
  --source-namespace source-ns \
  --source old-pvc \
  --dest-context dest-eks \
  --dest-namespace dest-ns \
  --dest new-pvc \
  --strategies loadbalancer \
  --helm-values aws-values.yaml \
  --helm-timeout 10m \
  --loadbalancer-timeout 10m
```

The timeout values above are example budgets, not AWS provisioning guarantees.
`--helm-timeout` controls Helm install/uninstall operations and the explicit sshd readiness wait used when Helm's wait is skipped for a LoadBalancer Service.
`--loadbalancer-timeout` separately controls how long pv-migrate waits for the Service to receive an address.
When no address arrives within that budget, the strategy falls back to the Service's node port on the sshd pod's node, which must be reachable from the rsync side.
This fallback still applies when `--strategies loadbalancer` is the only selected strategy.
Receiving a hostname does not prove that DNS resolves it or that SSH is reachable; subsequent rsync attempts handle connection failures.

Two optional flags need separate consideration:

- `--ignore-mounted` bypasses the check that rejects PVCs mounted by other pods.
  Use it only when workloads have been quiesced but their pods must remain; it does not stop writes, guarantee consistency, or override volume attachment constraints.
- `--dest-delete-extraneous-files` enables rsync's `--delete`, removing destination files absent from the source within the transfer scope.
  It does not erase the entire destination before copying, but can delete data you intended to retain.

## Troubleshooting

### Service address stays pending

Inspect the Service events and the controller logs in the cluster hosting sshd for provisioning, annotation, subnet, or permission errors.
Increase `--loadbalancer-timeout` if address allocation needs more time; increasing only `--helm-timeout` does not extend this wait.
If the node-port fallback is used, check node reachability from the rsync side or correct load balancer provisioning and retry.
For an earlier install or sshd readiness failure, inspect pod scheduling, volume attachment, and image pulls before adjusting `--helm-timeout`.

### Hostname exists but does not resolve

Check DNS resolution from the rsync cluster, including pod DNS access.
The Service address wait ends when a hostname is published, before DNS necessarily becomes usable.
The rsync retry settings above allow for transient DNS propagation failures; neither Helm nor address-allocation timeouts extend this retry window.
Persistent failures need DNS investigation rather than more retries alone.

### SSH endpoint is unreachable

Check the rsync pod logs, load balancer target health, and TCP connectivity from the rsync side.
Verify that the chosen internal or internet-facing scheme fits the routing between the clusters, and check security groups, network ACLs, node ports, and pod network policies along that path.
Successful DNS resolution alone does not establish connectivity.
Use `--rsync-push` if only the destination can expose a reachable Service.

### Connection drops while idle

A [participant in issue #257](https://github.com/utkuozdemir/pv-migrate/issues/257#issuecomment-2536009237) reported disconnects while re-syncing a large directory tree and resolved them in their environment with `service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout: "3600"` under `sshd.service.annotations`.
This is a controller-dependent observation, not an annotation guaranteed to configure the NLB example above or every EKS mode.
Confirm the supported idle-timeout mechanism for your actual controller, load balancer, and listener, then verify the effective setting in AWS.
Do not assume all AWS load balancers have the same idle timeout.

The bundled [sshd configuration](../docker/sshd/sshd_config) sets `ClientAliveInterval 300` and `ClientAliveCountMax 3`.
An intermediary that drops idle connections before the keepalive interval can terminate the connection first.
Compare the effective network idle timeout with those keepalives when diagnosing disconnects; increasing `--helm-timeout` or `--loadbalancer-timeout` does not keep an established SSH connection alive.

Use `--no-cleanup-on-failure` to retain resources for inspection, and follow the [cleanup instructions](usage.md#cleanup) afterward.
See the [CLI reference](cli-reference.md#root-command) and [chart values](../internal/helm/pv-migrate/values.yaml) for further configuration.
