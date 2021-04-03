package rsyncsshincluster

import (
	"errors"
	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/common"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RsyncSSSHInCluster struct {
}

func (r *RsyncSSSHInCluster) Cleanup(task task.Task) error {
	var result *multierror.Error
	err := k8s.CleanupForID(task.Source().KubeClient(), task.Source().Claim().Namespace, task.ID())
	if err != nil {
		result = multierror.Append(result, err)
	}
	err = k8s.CleanupForID(task.Dest().KubeClient(), task.Dest().Claim().Namespace, task.ID())
	if err != nil {
		result = multierror.Append(result, err)
	}
	//goland:noinspection GoNilness
	return result.ErrorOrNil()
}

func (r *RsyncSSSHInCluster) Name() string {
	return "rsync-ssh-in-cluster"
}

func (r *RsyncSSSHInCluster) Priority() int {
	return 2000
}

func (r *RsyncSSSHInCluster) CanDo(task task.Task) bool {
	sameCluster := task.Source().KubeClient() == task.Dest().KubeClient()
	return sameCluster
}

func (r *RsyncSSSHInCluster) Run(task task.Task) error {
	if !r.CanDo(task) {
		return errors.New("cannot do this task using this strategy")
	}

	instance := task.ID()
	sourcePvcInfo := task.Source()
	sourceKubeClient := task.Source().KubeClient()
	destKubeClient := task.Dest().KubeClient()
	sftpPod := rsync.PrepareSshdPod(instance, sourcePvcInfo)
	err := rsync.CreateSshdPodWaitTillRunning(sourceKubeClient, sftpPod)
	if err != nil {
		return err
	}
	createdService, err := rsync.CreateSshdService(instance, sourcePvcInfo)
	if err != nil {
		return err
	}
	targetServiceAddress, err := k8s.GetServiceAddress(createdService, sourceKubeClient)
	if err != nil {
		return err
	}
	log.Infof("use service address %s to connect to rsync server", targetServiceAddress)
	rsyncJob := buildRsyncJob(task, targetServiceAddress)
	err = k8s.CreateJobWaitTillCompleted(destKubeClient, rsyncJob)
	if err != nil {
		return err
	}
	return nil
}

func buildRsyncJob(task task.Task, targetHost string) batchv1.Job {
	jobTTLSeconds := int32(600)
	backoffLimit := int32(0)
	instance := task.ID()
	jobName := "pv-migrate-rsync-" + instance
	destPvcInfo := task.Dest()

	rsyncCommand := rsync.BuildRsyncCommand(task.Options().DeleteExtraneousFiles(), &targetHost)
	log.WithField("rsyncCommand", rsyncCommand).Info("Built rsync command")
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
					Labels: map[string]string{
						common.AppLabelKey:      common.AppLabelValue,
						common.InstanceLabelKey: instance,
						"component":             "rsync",
					},
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
					NodeName:      destPvcInfo.MountedNode(),
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
	return job
}
