package rsync

import (
	"context"
	"fmt"
	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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

func Cleanup(kubeClient kubernetes.Interface, instance string, namespace string) error {
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
