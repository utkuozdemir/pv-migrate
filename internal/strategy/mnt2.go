package strategy

import (
	"github.com/utkuozdemir/pv-migrate/internal/job"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Mnt2 struct {
}

func (r *Mnt2) canDo(job job.Job) bool {
	sameCluster := job.Source().KubeClient() == job.Dest().KubeClient()
	if !sameCluster {
		return false
	}

	sameNamespace := job.Source().Claim().Namespace == job.Dest().Claim().Namespace
	if !sameNamespace {
		return false
	}

	sameNode := job.Source().MountedNode() == job.Dest().MountedNode()
	return sameNode || job.Source().SupportsROX() || job.Source().SupportsRWX() || job.Dest().SupportsRWX()
}

func (r *Mnt2) Run(task task.Task) (bool, error) {
	if !r.canDo(task.Job()) {
		return false, nil
	}

	node := determineTargetNode(task.Job())
	migrationJob, err := buildRsyncJob(task, node)
	if err != nil {
		return true, err
	}

	defer cleanup(task)
	return true, k8s.CreateJobWaitTillCompleted(task.Job().Source().KubeClient(), migrationJob)
}

func determineTargetNode(job job.Job) string {
	if (job.Source().SupportsROX() || job.Source().SupportsRWX()) && job.Dest().SupportsRWX() {
		return ""
	}
	if !job.Source().SupportsROX() && !job.Source().SupportsRWX() {
		return job.Source().MountedNode()
	}
	return job.Dest().MountedNode()
}

func buildRsyncJob(task task.Task, node string) (*batchv1.Job, error) {
	jobTTLSeconds := int32(600)
	backoffLimit := int32(0)
	id := task.ID()
	jobName := "pv-migrate-rsync-" + id
	migrationJob := task.Job()
	rsyncScript, err := rsync.BuildRsyncScript(migrationJob.Options().DeleteExtraneousFiles(),
		migrationJob.Options().NoChown(),
		"")
	if err != nil {
		return nil, err
	}
	k8sJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: migrationJob.Dest().Claim().Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &jobTTLSeconds,
			Template: corev1.PodTemplateSpec{

				ObjectMeta: metav1.ObjectMeta{
					Name:      jobName,
					Namespace: migrationJob.Dest().Claim().Namespace,
					Labels:    k8s.Labels(id),
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "source-vol",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: migrationJob.Source().Claim().Name,
								},
							},
						},
						{
							Name: "dest-vol",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: migrationJob.Dest().Claim().Name,
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
	return &k8sJob, nil
}
