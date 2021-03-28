package rsync

import (
	"context"
	"fmt"
	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func buildRsyncCommand(task *migration.Task, targetHost string) []string {
	rsyncCommand := []string{"rsync"}
	if task.Options.DeleteExtraneousFiles {
		rsyncCommand = append(rsyncCommand, "--delete")
	}
	rsyncCommand = append(rsyncCommand, "-avzh")
	rsyncCommand = append(rsyncCommand, "--progress")
	rsyncCommand = append(rsyncCommand, "-e")
	rsyncCommand = append(rsyncCommand, "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null")
	rsyncCommand = append(rsyncCommand, fmt.Sprintf("root@%s:/source/", targetHost))
	rsyncCommand = append(rsyncCommand, "/dest/")
	return rsyncCommand
}

func prepareRsyncJob(task *migration.Task, targetHost string) batchv1.Job {
	jobTtlSeconds := int32(600)
	backoffLimit := int32(0)
	instance := task.Id
	jobName := "pv-migrate-rsync-" + instance
	destPvcInfo := task.Dest

	rsyncCommand := buildRsyncCommand(task, targetHost)
	log.WithField("rsyncCommand", rsyncCommand).Info("Built rsync command")
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: destPvcInfo.Claim.Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &jobTtlSeconds,
			Template: corev1.PodTemplateSpec{

				ObjectMeta: metav1.ObjectMeta{
					Name:      jobName,
					Namespace: destPvcInfo.Claim.Namespace,
					Labels: map[string]string{
						"app":       "pv-migrate",
						"component": "rsync",
						"instance":  instance,
					},
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "dest-vol",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: destPvcInfo.Claim.Name,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "app",
							Image:           "docker.io/utkuozdemir/pv-migrate-rsync:v0.1.0",
							ImagePullPolicy: corev1.PullAlways,
							Command:         rsyncCommand,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "dest-vol",
									MountPath: "/dest",
								},
							},
						},
					},
					NodeName:      destPvcInfo.MountedNode,
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
	return job
}

func MigrateViaRsync(task *migration.Task) error {
	instance := task.Id
	sourcePvcInfo := task.Source
	sourceKubeClient := task.Source.KubeClient
	destKubeClient := task.Dest.KubeClient
	sftpPod := prepareSshdPod(instance, sourcePvcInfo)
	err := createSshdPodWaitTillRunning(sourceKubeClient, sftpPod)
	if err != nil {
		return err
	}
	createdService, err := createSshdService(instance, sourceKubeClient, sourcePvcInfo)
	if err != nil {
		return err
	}
	targetServiceAddress, err := k8s.GetServiceAddress(createdService, sourceKubeClient)
	if err != nil {
		return err
	}
	log.Infof("use service address %s to connect to rsync server", targetServiceAddress)
	rsyncJob := prepareRsyncJob(task, targetServiceAddress)
	err = k8s.CreateJobWaitTillCompleted(destKubeClient, rsyncJob)
	if err != nil {
		return err
	}
	return nil
}

func Cleanup(kubeClient *kubernetes.Clientset, instance string, namespace string) error {
	log.WithFields(log.Fields{
		"instance":  instance,
		"namespace": namespace,
	}).Info("Doing cleanup")

	var result *multierror.Error
	err := kubeClient.BatchV1().Jobs(namespace).DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app=pv-migrate,instance=" + instance,
	})
	if err != nil {
		result = multierror.Append(result, err)
	}

	err = kubeClient.CoreV1().Pods(namespace).DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app=pv-migrate,instance=" + instance,
	})
	if err != nil {
		result = multierror.Append(result, err)
	}

	serviceClient := kubeClient.CoreV1().Services(namespace)
	serviceList, err := serviceClient.List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app=pv-migrate,instance=" + instance,
	})
	if err != nil {
		result = multierror.Append(result, err)
	}

	for _, service := range serviceList.Items {
		err = serviceClient.Delete(context.TODO(), service.Name, metav1.DeleteOptions{})
		if err != nil {
			result = multierror.Append(result, err)
		}
	}

	log.WithFields(log.Fields{
		"instance": instance,
	}).Info("Finished cleanup")

	//goland:noinspection GoNilness
	return result.ErrorOrNil()
}
