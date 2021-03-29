package migration

import (
	"errors"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/constants"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MountBothStrategy struct {
}

func (r *MountBothStrategy) Cleanup(task Task) error {
	return k8s.CleanupForId(task.Source().KubeClient(), task.Source().Claim().Namespace, task.Id())
}

func (r *MountBothStrategy) Name() string {
	return "mount-both"
}

func (r *MountBothStrategy) Priority() int {
	return 1000
}

func (r *MountBothStrategy) CanDo(task Task) bool {
	sameCluster := task.Source().KubeClient() == task.Dest().KubeClient()
	if !sameCluster {
		return false
	}

	sameNamespace := task.Source().Claim().Namespace == task.Dest().Claim().Namespace
	if !sameNamespace {
		return false
	}

	sameNode := task.Source().MountedNode() == task.Dest().MountedNode()
	return sameNode || task.Source().SupportsROX() || task.Source().SupportsRWX() || task.Dest().SupportsRWX()
}

func (r *MountBothStrategy) Run(task Task) error {
	if !r.CanDo(task) {
		return errors.New("cannot do this task using this strategy")
	}

	node := determineTargetNode(task)
	job := buildRsyncJob(task, node)
	return k8s.CreateJobWaitTillCompleted(task.Source().KubeClient(), job)
}

func determineTargetNode(task Task) string {
	if (task.Source().SupportsROX() || task.Source().SupportsRWX()) && task.Dest().SupportsRWX() {
		return ""
	}
	if !task.Source().SupportsROX() && !task.Source().SupportsRWX() {
		return task.Source().MountedNode()
	}
	return task.Dest().MountedNode()
}

func buildRsyncJob(task Task, node string) batchv1.Job {
	jobTtlSeconds := int32(600)
	backoffLimit := int32(0)
	instance := task.Id()
	jobName := "pv-migrate-rsync-" + instance
	rsyncCommand := rsync.BuildRsyncCommand(task.Options().DeleteExtraneousFiles(), nil)
	log.WithField("rsyncCommand", rsyncCommand).Info("Built rsync command")
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: task.Dest().Claim().Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &jobTtlSeconds,
			Template: corev1.PodTemplateSpec{

				ObjectMeta: metav1.ObjectMeta{
					Name:      jobName,
					Namespace: task.Dest().Claim().Namespace,
					Labels: map[string]string{
						constants.AppLabelKey:      constants.AppLabelValue,
						constants.InstanceLabelKey: instance,
					},
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "source-vol",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: task.Source().Claim().Name,
								},
							},
						},
						{
							Name: "dest-vol",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: task.Dest().Claim().Name,
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
									Name:      "source-vol",
									MountPath: "/source",
								},
								{
									Name:      "dest-vol",
									MountPath: "/dest",
								},
							},
						},
					},
					NodeName:      node,
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
	return job
}
