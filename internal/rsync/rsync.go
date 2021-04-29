package rsync

import (
	"bytes"
	"context"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"html/template"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	maxRetries            = 10
	retryIntervalSecs     = 5
	sshConnectTimeoutSecs = 5
)

var scriptTemplate = template.Must(template.New("script").Parse(`
n=0
rc=1
retries={{.MaxRetries}}
until [ "$n" -ge "$retries" ]
do
  rsync \
    -avzh \
    --progress \
    {{ if .DeleteExtraneousFiles -}}
    --delete \
    {{ end -}}
    {{ if .NoChown -}}
    --no-o --no-g \
    {{ end -}}
    {{ if .SshTargetHost -}}
    -e "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout={{.SshConnectTimeoutSecs}}" \
    root@{{.SshTargetHost}}:/source/ \
    {{ else -}}
    /source/ \
    {{ end -}}
    /dest/ && \
    rc=0 && \
    break
  n=$((n+1))
  echo "rsync attempt $n/{{.MaxRetries}} failed, waiting {{.RetryIntervalSecs}} seconds before trying again"
  sleep {{.RetryIntervalSecs}}
done

if [ $rc -ne 0 ]; then
  echo "Rsync job failed after $retries retries"
fi
exit $rc
`))

type script struct {
	MaxRetries            int
	DeleteExtraneousFiles bool
	NoChown               bool
	SshTargetHost         string
	SshConnectTimeoutSecs int
	RetryIntervalSecs     int
}

func BuildRsyncScript(deleteExtraneousFiles bool, noChown bool, sshTargetHost string) (string, error) {
	s := script{
		MaxRetries:            maxRetries,
		DeleteExtraneousFiles: deleteExtraneousFiles,
		NoChown:               noChown,
		SshTargetHost:         sshTargetHost,
		SshConnectTimeoutSecs: sshConnectTimeoutSecs,
		RetryIntervalSecs:     retryIntervalSecs,
	}

	var templatedScript bytes.Buffer
	err := scriptTemplate.Execute(&templatedScript, s)
	if err != nil {
		return "", err
	}

	return templatedScript.String(), nil
}

func createRsyncPrivateKeySecret(instanceId string, pvcInfo pvc.Info, privateKey string) (*corev1.Secret, error) {
	kubeClient := pvcInfo.KubeClient()
	namespace := pvcInfo.Claim().Namespace
	name := "pv-migrate-rsync-" + instanceId
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    k8s.ComponentLabels(instanceId, k8s.Rsync),
		},
		Data: map[string][]byte{
			"privateKey": []byte(privateKey),
		},
	}

	secrets := kubeClient.CoreV1().Secrets(namespace)
	return secrets.Create(context.TODO(), &secret, metav1.CreateOptions{})
}

func buildRsyncJobDest(task task.Task, targetHost string, privateKeySecretName string) (*batchv1.Job, error) {
	jobTTLSeconds := int32(600)
	backoffLimit := int32(0)
	id := task.ID()
	jobName := "pv-migrate-rsync-" + id
	migrationJob := task.Job()
	destPvcInfo := migrationJob.Dest()

	rsyncScript, err := BuildRsyncScript(migrationJob.Options().DeleteExtraneousFiles(),
		migrationJob.Options().NoChown(), targetHost)
	if err != nil {
		return nil, err
	}

	permissions := int32(256) // octal mode 0400 - we don't need more than that
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: destPvcInfo.Claim().Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &jobTTLSeconds,
			Template: corev1.PodTemplateSpec{

				ObjectMeta: metav1.ObjectMeta{
					Name:      jobName,
					Namespace: destPvcInfo.Claim().Namespace,
					Labels:    k8s.ComponentLabels(id, k8s.Rsync),
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "dest-vol",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: destPvcInfo.Claim().Name,
								},
							},
						},
						{
							Name: "private-key-vol",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName:  privateKeySecretName,
									DefaultMode: &permissions,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: task.Job().RsyncImage(),
							Command: []string{
								"sh",
								"-c",
								rsyncScript,
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "dest-vol",
									MountPath: "/dest",
								},
								{
									Name:      "private-key-vol",
									MountPath: "/root/.ssh/id_rsa",
									SubPath:   "privateKey",
								},
							},
						},
					},
					NodeName:      destPvcInfo.MountedNode(),
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
	return &job, nil
}

func RunRsyncJobOverSsh(task task.Task, serviceType corev1.ServiceType) error {
	instanceId := task.ID()
	migrationJob := task.Job()
	sourcePvcInfo := migrationJob.Source()
	sourceKubeClient := migrationJob.Source().KubeClient()
	destPvcInfo := migrationJob.Dest()
	destKubeClient := destPvcInfo.KubeClient()

	log.Info("Generating RSA SSH key pair")
	publicKey, privateKey, err := ssh.CreateSSHKeyPair()
	if err != nil {
		return err
	}

	log.Info("Creating secret for the public key")
	secret, err := createSshdPublicKeySecret(instanceId, sourcePvcInfo, publicKey)
	if err != nil {
		return err
	}

	sftpPod := PrepareSshdPod(instanceId, sourcePvcInfo, secret.Name, task.Job().SshdImage())
	err = CreateSshdPodWaitTillRunning(sourceKubeClient, sftpPod)
	if err != nil {
		return err
	}

	createdService, err := CreateSshdService(instanceId, sourcePvcInfo, serviceType)
	if err != nil {
		return err
	}
	targetHost, err := k8s.GetServiceAddress(sourceKubeClient, createdService)
	if err != nil {
		return err
	}

	log.Info("Creating secret for the private key")
	secret, err = createRsyncPrivateKeySecret(instanceId, destPvcInfo, privateKey)
	if err != nil {
		return err
	}

	log.WithField("targetHost", targetHost).Info("Connecting to the rsync server")
	rsyncJob, err := buildRsyncJobDest(task, targetHost, secret.Name)
	if err != nil {
		return err
	}

	err = k8s.CreateJobWaitTillCompleted(destKubeClient, rsyncJob)
	if err != nil {
		return err
	}
	return nil
}
