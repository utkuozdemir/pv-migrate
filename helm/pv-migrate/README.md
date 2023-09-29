# pv-migrate

![Version: 0.4.0](https://img.shields.io/badge/Version-0.4.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.4.0](https://img.shields.io/badge/AppVersion-0.4.0-informational?style=flat-square)

The helm chart of pv-migrate

**Homepage:** <https://github.com/utkuozdemir/pv-migrate>

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| Utku Özdemir | <uoz@protonmail.com> | <https://utkuozdemir.org> |

## Source Code

* <https://github.com/utkuozdemir/pv-migrate>

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| fullnameOverride | string | `""` | String to fully override the fullname template with a string |
| nameOverride | string | `""` | String to partially override the fullname template with a string (will prepend the release name) |
| rsync.affinity | object | `{}` | Rsync pod affinity |
| rsync.backoffLimit | int | `0` |  |
| rsync.command | string | `""` | Full Rsync command and flags |
| rsync.enabled | bool | `false` | Enable creation of Rsync job |
| rsync.extraArgs | string | `""` | Extra args to be appended to the rsync command. Setting this might cause the tool to not function properly. |
| rsync.fixPrivateKeyPerms | bool | `false` | Enable fixing permissions on the private key prior to running rsync |
| rsync.image.pullPolicy | string | `"IfNotPresent"` | Rsync image pull policy |
| rsync.image.repository | string | `"docker.io/utkuozdemir/pv-migrate-rsync"` | Rsync image repository |
| rsync.image.tag | string | `"1.0.0"` | Rsync image tag |
| rsync.imagePullSecrets | list | `[]` | Rsync image pull secrets |
| rsync.maxRetries | int | `10` | Number of retries to run rsync command |
| rsync.namespace | string | `""` | Namespace to run Rsync pod in |
| rsync.networkPolicy.enabled | bool | `false` | Enable Rsync network policy |
| rsync.nodeName | string | `""` | The node name to schedule Rsync pod on |
| rsync.nodeSelector | object | `{}` | Rsync node selector |
| rsync.podAnnotations | object | `{}` | Rsync pod annotations |
| rsync.podSecurityContext | object | `{}` | Rsync pod security context |
| rsync.privateKey | string | `""` | The private key content |
| rsync.privateKeyMount | bool | `false` | Mount a private key into the Rsync pod |
| rsync.privateKeyMountPath | string | `"/root/.ssh/id_ed25519"` | The path to mount the private key |
| rsync.pvcMounts | list | `[]` | PVC mounts into the Rsync pod. For examples, see [values.yaml](values.yaml) |
| rsync.resources | object | `{}` | Rsync pod resources |
| rsync.restartPolicy | string | `"Never"` |  |
| rsync.retryPeriodSeconds | int | `5` | Waiting time between retries |
| rsync.securityContext | object | `{}` | Rsync deployment security context |
| rsync.serviceAccount.annotations | object | `{}` | Rsync service account annotations |
| rsync.serviceAccount.create | bool | `true` | Create a service account for Rsync |
| rsync.serviceAccount.name | string | `""` | Rsync service account name to use |
| rsync.tolerations | list | see [values.yaml](values.yaml) | Rsync pod tolerations |
| sshd.affinity | object | `{}` | SSHD pod affinity |
| sshd.enabled | bool | `false` | Enable SSHD server deployment |
| sshd.image.pullPolicy | string | `"IfNotPresent"` | SSHD image pull policy |
| sshd.image.repository | string | `"docker.io/utkuozdemir/pv-migrate-sshd"` | SSHD image repository |
| sshd.image.tag | string | `"1.0.0"` | SSHD image tag |
| sshd.imagePullSecrets | list | `[]` | SSHD image pull secrets |
| sshd.namespace | string | `""` | Namespace to run SSHD pod in |
| sshd.networkPolicy.enabled | bool | `false` | Enable SSHD network policy |
| sshd.nodeName | string | `""` | The node name to schedule SSHD pod on |
| sshd.nodeSelector | object | `{}` | SSHD node selector |
| sshd.podAnnotations | object | `{}` | SSHD pod annotations |
| sshd.podSecurityContext | object | `{}` | SSHD pod security context |
| sshd.privateKey | string | `""` | The private key content |
| sshd.privateKeyMount | bool | `false` | Mount a private key into the SSHD pod |
| sshd.privateKeyMountPath | string | `"/root/.ssh/id_ed25519"` | The path to mount the private key |
| sshd.publicKey | string | `""` | The public key content |
| sshd.publicKeyMount | bool | `true` | Mount a public key into the SSHD pod |
| sshd.publicKeyMountPath | string | `"/root/.ssh/authorized_keys"` | The path to mount the public key |
| sshd.pvcMounts | list | `[]` | PVC mounts into the SSHD pod. For examples, see see [values.yaml](values.yaml) |
| sshd.resources | object | `{}` | SSHD pod resources |
| sshd.securityContext | object | `{"capabilities":{"add":["SYS_CHROOT"]}}` | SSHD deployment security context |
| sshd.service.annotations | object | `{}` | SSHD service annotations |
| sshd.service.loadBalancerIP | string | `""` | SSHD service load balancer IP |
| sshd.service.port | int | `22` | SSHD service port |
| sshd.service.type | string | `"ClusterIP"` | SSHD service type |
| sshd.serviceAccount.annotations | object | `{}` | SSHD service account annotations |
| sshd.serviceAccount.create | bool | `true` | Create a service account for SSHD |
| sshd.serviceAccount.name | string | `""` | SSHD service account name to use |
| sshd.tolerations | list | see [values.yaml](values.yaml) | SSHD pod tolerations |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.11.2](https://github.com/norwoodj/helm-docs/releases/v1.11.2)
