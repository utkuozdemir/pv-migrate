package rsync

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
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

func buildRsyncJobDest(task task.Task, targetHost string) batchv1.Job {
	jobTTLSeconds := int32(600)
	backoffLimit := int32(0)
	id := task.ID()
	jobName := "pv-migrate-rsync-" + id
	destPvcInfo := task.Dest()

	rsyncCommand := BuildRsyncCommand(task.Options().DeleteExtraneousFiles(), &targetHost)
	log.WithField("rsyncCommand", strings.Join(rsyncCommand, " ")).Info("Built rsync command")
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
					},
					Containers: []corev1.Container{
						{
							Name:            "app",
							Image:           "docker.io/utkuozdemir/pv-migrate-rsync:v0.1.0",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         rsyncCommand,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "dest-vol",
									MountPath: "/dest",
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
	destKubeClient := task.Dest().KubeClient()
	sftpPod := PrepareSshdPod(instanceId, sourcePvcInfo)
	err := CreateSshdPodWaitTillRunning(sourceKubeClient, sftpPod)
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
	log.WithField("targetHost", targetHost).Info("Connect to rsync server")
	rsyncJob := buildRsyncJobDest(task, targetHost)
	err = k8s.CreateJobWaitTillCompleted(destKubeClient, rsyncJob)
	if err != nil {
		return err
	}
	return nil
}
