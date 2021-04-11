package rsync

import (
	"context"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
)

func BuildRsyncCommand(deleteExtraneousFiles bool, sshTargetHost *string) []string {
	rsyncCommand := []string{"rsync"}
	if deleteExtraneousFiles {
		rsyncCommand = append(rsyncCommand, "--delete")
	}
	rsyncCommand = append(rsyncCommand, "-avzh")
	rsyncCommand = append(rsyncCommand, "--progress")

	if sshTargetHost != nil {
		rsyncCommand = append(rsyncCommand, "-e")
		rsyncCommand = append(rsyncCommand, "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null")
		rsyncCommand = append(rsyncCommand, fmt.Sprintf("root@%s:/source/", *sshTargetHost))
	} else {
		rsyncCommand = append(rsyncCommand, "/source/")
	}

	rsyncCommand = append(rsyncCommand, "/dest/")
	return rsyncCommand
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

func buildRsyncJobDest(task task.Task, targetHost string, privateKeySecretName string) batchv1.Job {
	jobTTLSeconds := int32(600)
	backoffLimit := int32(0)
	id := task.ID()
	jobName := "pv-migrate-rsync-" + id
	destPvcInfo := task.Dest()

	rsyncCommand := BuildRsyncCommand(task.Options().DeleteExtraneousFiles(), &targetHost)
	log.WithField("rsyncCommand", strings.Join(rsyncCommand, " ")).Info("Built rsync command")
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
							Name:    "app",
							Image:   "docker.io/instrumentisto/rsync-ssh:alpine",
							Command: rsyncCommand,
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
	return job
}

func RunRsyncJobOverSsh(task task.Task, serviceType corev1.ServiceType) error {
	instanceId := task.ID()
	sourcePvcInfo := task.Source()
	sourceKubeClient := task.Source().KubeClient()
	destPvcInfo := task.Dest()
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

	sftpPod := PrepareSshdPod(instanceId, sourcePvcInfo, secret.Name)
	err = CreateSshdPodWaitTillRunning(sourceKubeClient, sftpPod)
	if err != nil {
		return err
	}

	createdService, err := CreateSshdService(instanceId, sourcePvcInfo, serviceType)
	if err != nil {
		return err
	}
	targetHost, err := k8s.GetServiceAddress(createdService, sourceKubeClient)
	if err != nil {
		return err
	}

	log.Info("Creating secret for the private key")
	secret, err = createRsyncPrivateKeySecret(instanceId, destPvcInfo, privateKey)
	if err != nil {
		return err
	}

	log.WithField("targetHost", targetHost).Info("Connecting to the rsync server")
	rsyncJob := buildRsyncJobDest(task, targetHost, secret.Name)
	err = k8s.CreateJobWaitTillCompleted(destKubeClient, rsyncJob)
	if err != nil {
		return err
	}
	return nil
}
