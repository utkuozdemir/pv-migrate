package rsync

import (
	"bytes"
	"context"
	"fmt"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
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
    -azv \
    --info=progress2,misc0,flist0 \
    --no-inc-recursive \
    {{ if .DeleteExtraneousFiles -}}
    --delete \
    {{ end -}}
    {{ if .NoChown -}}
    --no-o --no-g \
    {{ end -}}
    {{ if .SshTargetHost -}}
    -e "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout={{.SshConnectTimeoutSecs}}" \
    root@{{.SshTargetHost}}:/source/{{.SourcePath}} \
    {{ else -}}
    /source/{{.SourcePath}} \
    {{ end -}}
    /dest/{{.DestPath}} && \
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
	SourcePath            string
	DestPath              string
}

func BuildRsyncScript(deleteExtraneousFiles bool, noChown bool,
	sshTargetHost string, sourcePath string, destPath string) (string, error) {
	s := script{
		MaxRetries:            maxRetries,
		DeleteExtraneousFiles: deleteExtraneousFiles,
		NoChown:               noChown,
		SshTargetHost:         sshTargetHost,
		SshConnectTimeoutSecs: sshConnectTimeoutSecs,
		RetryIntervalSecs:     retryIntervalSecs,
		SourcePath:            sourcePath,
		DestPath:              destPath,
	}

	var templatedScript bytes.Buffer
	err := scriptTemplate.Execute(&templatedScript, s)
	if err != nil {
		return "", err
	}

	return templatedScript.String(), nil
}

func createRsyncPrivateKeySecret(instanceId string, pvcInfo *pvc.Info, privateKey string) (*corev1.Secret, error) {
	kubeClient := pvcInfo.KubeClient
	namespace := pvcInfo.Claim.Namespace
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

func buildRsyncJobDest(e *task.Execution, targetHost string, privateKeySecretName string,
	sourcePath string, destPath string) (*batchv1.Job, error) {
	t := e.Task
	jobTTLSeconds := int32(600)
	backoffLimit := int32(0)
	id := e.ID
	jobName := "pv-migrate-rsync-" + id
	d := t.DestInfo

	opts := t.Migration.Options
	rsyncScript, err := BuildRsyncScript(opts.DeleteExtraneousFiles,
		opts.NoChown, targetHost, sourcePath, destPath)
	if err != nil {
		return nil, err
	}

	permissions := int32(256) // octal mode 0400 - we don't need more than that
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: d.Claim.Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &jobTTLSeconds,
			Template: corev1.PodTemplateSpec{

				ObjectMeta: metav1.ObjectMeta{
					Name:      jobName,
					Namespace: d.Claim.Namespace,
					Labels:    k8s.ComponentLabels(id, k8s.Rsync),
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "dest-vol",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: d.Claim.Name,
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
							Image: t.Migration.RsyncImage,
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
									MountPath: fmt.Sprintf("/root/.ssh/id_%s", t.Migration.Options.KeyAlgorithm),
									SubPath:   "privateKey",
								},
							},
						},
					},
					NodeName:           d.MountedNode,
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: t.Migration.RsyncServiceAccount,
				},
			},
		},
	}
	return &job, nil
}

func RunRsyncJobOverSSH(e *task.Execution, serviceType corev1.ServiceType) error {
	logger := e.Logger
	instanceId := e.ID
	t := e.Task
	s := t.SourceInfo
	d := t.DestInfo
	sourceKubeClient := s.KubeClient
	destKubeClient := d.KubeClient

	logger.Info(":key: Generating SSH key pair")
	publicKey, privateKey, err := CreateSSHKeyPair(t.Migration.Options.KeyAlgorithm)
	if err != nil {
		return err
	}

	logger.Info(":key: Creating secret for the public key")
	secret, err := createSshdPublicKeySecret(instanceId, s, publicKey)
	if err != nil {
		return err
	}

	sftpPod := PrepareSshdPod(
		instanceId,
		s,
		secret.Name,
		t.Migration.SshdImage,
		t.Migration.SshdServiceAccount,
		t.Migration.Options.SourceMountReadOnly,
	)
	err = CreateSshdPodWaitTillRunning(logger, sourceKubeClient, sftpPod)
	if err != nil {
		return err
	}

	createdService, err := CreateSshdService(instanceId, s, serviceType)
	if err != nil {
		return err
	}
	targetHost, err := k8s.GetServiceAddress(logger, sourceKubeClient, createdService)
	if err != nil {
		return err
	}

	logger.Info(":key: Creating secret for the private key")
	secret, err = createRsyncPrivateKeySecret(instanceId, d, privateKey)
	if err != nil {
		return err
	}

	logger.WithField("targetHost", targetHost).
		Info(":link: Connecting to the rsync server")
	m := e.Task.Migration
	rsyncJob, err := buildRsyncJobDest(e, targetHost, secret.Name, m.Source.Path, m.Dest.Path)
	if err != nil {
		return err
	}

	err = k8s.CreateJobWaitTillCompleted(logger, destKubeClient, rsyncJob)
	if err != nil {
		return err
	}
	return nil
}
