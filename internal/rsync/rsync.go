package rsync

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func buildRsyncCommand(claimInfo k8s.ClaimInfo, targetHost string) []string {
	rsyncCommand := []string{"rsync"}
	if claimInfo.DeleteExtraneousFiles {
		rsyncCommand = append(rsyncCommand, "--delete")
	}
	rsyncCommand = append(rsyncCommand, "-avz")
	rsyncCommand = append(rsyncCommand, "-e")
	rsyncCommand = append(rsyncCommand, "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null")
	rsyncCommand = append(rsyncCommand, fmt.Sprintf("root@%s:/source/", targetHost))
	rsyncCommand = append(rsyncCommand, "/dest/")

	return rsyncCommand
}

func prepareRsyncJob(instance string, destClaimInfo k8s.ClaimInfo, targetHost string) batchv1.Job {
	jobTtlSeconds := int32(600)
	backoffLimit := int32(0)
	jobName := "pv-migrate-rsync-" + instance

	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: destClaimInfo.Claim.Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &jobTtlSeconds,
			Template: corev1.PodTemplateSpec{

				ObjectMeta: metav1.ObjectMeta{
					Name:      jobName,
					Namespace: destClaimInfo.Claim.Namespace,
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
									ClaimName: destClaimInfo.Claim.Name,
									ReadOnly:  destClaimInfo.ReadOnly,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "app",
							Image:           "docker.io/utkuozdemir/pv-migrate-rsync:v0.1.0",
							ImagePullPolicy: corev1.PullAlways,
							Command:         buildRsyncCommand(destClaimInfo, targetHost),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "dest-vol",
									MountPath: "/dest",
									ReadOnly:  destClaimInfo.ReadOnly,
								},
							},
						},
					},
					NodeName:      destClaimInfo.OwnerNode,
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
	return job
}

func MigrateViaRsync(instance string, sourcekubeClient *kubernetes.Clientset, destkubeClient *kubernetes.Clientset, sourceClaimInfo k8s.ClaimInfo, destClaimInfo k8s.ClaimInfo) {
	sftpPod := prepareSshdPod(instance, sourceClaimInfo)
	createSshdPodWaitTillRunning(sourcekubeClient, sftpPod)
	createdService := createSshdService(instance, sourcekubeClient, sourceClaimInfo)
	targetServiceAddress := k8s.GetServiceAddress(createdService, sourcekubeClient)

	log.Infof("use service address %s to connect to rsync server", targetServiceAddress)
	rsyncJob := prepareRsyncJob(instance, destClaimInfo, targetServiceAddress)
	k8s.CreateJobWaitTillCompleted(destkubeClient, rsyncJob)
}
