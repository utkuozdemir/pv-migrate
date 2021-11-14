# pv-migrate

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

The helm chart of pv-migrate

**Homepage:** <https://github.com/utkuozdemir/pv-migrate>

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| Utku Özdemir | uoz@protonmail.com | https://utkuozdemir.org |

## Source Code

* <https://github.com/utkuozdemir/pv-migrate>

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| dest.namespace | string | `""` | Namespace of the destination PVC |
| dest.path | string | `""` | The path in the destination volume to be migrated |
| dest.pvcName | string | `""` | Name of the destination PVC |
| fullnameOverride | string | `""` | String to fully override the fullname template with a string |
| nameOverride | string | `""` | String to partially override the fullname template with a string (will prepend the release name) |
| rsync.affinity | object | `{}` | Rsync pod affinity |
| rsync.backoffLimit | int | `0` |  |
| rsync.deleteExtraneousFiles | bool | `false` | Whether the Rsync job should delete the extraneous files or not (--delete). |
| rsync.enabled | bool | `false` | Enable creation of Rsync job |
| rsync.image.pullPolicy | string | `"IfNotPresent"` | Rsync image pull policy |
| rsync.image.repository | string | `"docker.io/utkuozdemir/pv-migrate-rsync"` | Rsync image repository |
| rsync.image.tag | string | `"1.0.0"` | Rsync image tag |
| rsync.imagePullSecrets | list | `[]` | Rsync image pull secrets |
| rsync.mountSource | bool | `false` | Mount the source PVC into the Rsync pod as well as destination PVC |
| rsync.noChown | bool | `false` |  |
| rsync.nodeName | string | `""` | The node name to schedule Rsync pod on |
| rsync.nodeSelector | object | `{}` | Rsync node selector |
| rsync.podAnnotations | object | `{}` | Rsync pod annotations |
| rsync.podSecurityContext | object | `{}` | Rsync pod security context |
| rsync.privateKey | string | `""` | The private key content |
| rsync.privateKeyMount | bool | `false` | Mount a private key into the Rsync pod |
| rsync.privateKeyMountPath | string | `"/root/.ssh/id_ed25519"` | The path to mount the private key |
| rsync.rawCommand | string | `"rsync -azv --info=progress2,misc0,flist0 --no-inc-recursive"` | Raw Rsync command and flags |
| rsync.resources | object | `{}` | Rsync pod resources |
| rsync.restartPolicy | string | `"Never"` |  |
| rsync.securityContext | object | `{}` | Rsync deployment security context |
| rsync.serviceAccount.annotations | object | `{}` | Rsync service account annotations |
| rsync.serviceAccount.create | bool | `true` | Create a service account for Rsync |
| rsync.serviceAccount.name | string | `""` | Rsync service account name to use |
| rsync.sshRawCommand | string | `"ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5"` | The raw command of SSH for Rsync over SSH |
| rsync.sshRemoteHost | string | `""` | The remote host address for the Rsync job over SSH |
| rsync.sshUser | string | `"root"` | The user to use for SSH when Rsync is initiated over SSH |
| rsync.tolerations | list | see [values.yaml](values.yaml) | Rsync pod tolerations |
| source.namespace | string | `""` | Namespace of the source PVC |
| source.path | string | `""` | The path in the source volume to be migrated |
| source.pvcMountReadOnly | bool | `true` | Whether to mount the source PVC in readOnly mode |
| source.pvcName | string | `""` | Name of the source PVC |
| sshd.affinity | object | `{}` | SSHD pod affinity |
| sshd.enabled | bool | `false` | Enable SSHD server deployment |
| sshd.image.pullPolicy | string | `"IfNotPresent"` | SSHD image pull policy |
| sshd.image.repository | string | `"docker.io/utkuozdemir/pv-migrate-sshd"` | SSHD image repository |
| sshd.image.tag | string | `"1.0.0"` | SSHD image tag |
| sshd.imagePullSecrets | list | `[]` | SSHD image pull secrets |
| sshd.nodeName | string | `""` | The node name to schedule SSHD pod on |
| sshd.nodeSelector | object | `{}` | SSHD node selector |
| sshd.podAnnotations | object | `{}` | SSHD pod annotations |
| sshd.podSecurityContext | object | `{}` | SSHD pod security context |
| sshd.publicKey | string | `""` | The public key content |
| sshd.publicKeyMount | bool | `true` | Mount a public key into the SSHD pod |
| sshd.publicKeyMountPath | string | `"/root/.ssh/authorized_keys"` | The path to mount the public key |
| sshd.resources | object | `{}` | SSHD pod resources |
| sshd.securityContext | object | `{}` | SSHD deployment security context |
| sshd.service.port | int | `22` | SSHD service port |
| sshd.service.type | string | `"ClusterIP"` | SSHD service type |
| sshd.serviceAccount.annotations | object | `{}` | SSHD service account annotations |
| sshd.serviceAccount.create | bool | `true` | Create a service account for SSHD |
| sshd.serviceAccount.name | string | `""` | SSHD service account name to use |
| sshd.tolerations | list | see [values.yaml](values.yaml) | SSHD pod tolerations |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.5.0](https://github.com/norwoodj/helm-docs/releases/v1.5.0)
