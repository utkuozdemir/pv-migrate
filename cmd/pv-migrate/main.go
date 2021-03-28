package main

import (
	"context"
	"flag"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	// needed for k8s oidc and gcp auth
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

func init() {
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)
	rand.Seed(time.Now().UnixNano())
}

var letters = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

func randSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func doCleanup(kubeClient *kubernetes.Clientset, instance string, namespace string) {
	log.WithFields(log.Fields{
		"instance":  instance,
		"namespace": namespace,
	}).Info("Doing cleanup")

	_ = kubeClient.BatchV1().Jobs(namespace).DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app=pv-migrate,instance=" + instance,
	})

	_ = kubeClient.CoreV1().Pods(namespace).DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app=pv-migrate,instance=" + instance,
	})

	serviceClient := kubeClient.CoreV1().Services(namespace)
	serviceList, _ := serviceClient.List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app=pv-migrate,instance=" + instance,
	})

	for _, service := range serviceList.Items {
		_ = serviceClient.Delete(context.TODO(), service.Name, metav1.DeleteOptions{})
	}
	log.WithFields(log.Fields{
		"instance": instance,
	}).Info("Finished cleanup")
}

func buildConfigFromFlags(context, kubeconfigPath string) (*rest.Config, error) {
	clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfigLoadingRules := clientcmd.NewDefaultClientConfigLoadingRules()

	if kubeconfigPath != "" {
		clientConfigLoadingRules = &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientConfigLoadingRules,
		&clientcmd.ConfigOverrides{
			CurrentContext: context,
		}).ClientConfig()
}

func main() {
	kubeconfig := flag.String("kubeconfig", "", "(optional) absolute path to the kubeconfig file")
	source := flag.String("source", "", "Source persistent volume claim")
	sourceNamespace := flag.String("source-namespace", "", "Source namespace")
	sourceContext := flag.String("source-context", "", "(optional) Source context")
	dest := flag.String("dest", "", "Destination persistent volume claim")
	destNamespace := flag.String("dest-namespace", "", "Destination namespace")
	destContext := flag.String("dest-context", "", "(optional) Destination context")
	sourceReadOnly := flag.Bool("sourceReadOnly", true, "(optional) source pvc ReadOnly")
	deleteExtraneousFromDest := flag.Bool("dest-delete-extraneous-files", false, "(optional) delete extraneous files from destination dirs")
	flag.Parse()

	if *deleteExtraneousFromDest {
		log.Warn("delete extraneous files from dest is enabled")
	}

	if *source == "" || *sourceNamespace == "" || *dest == "" || *destNamespace == "" {
		flag.Usage()
		return
	}

	svcType := corev1.ServiceTypeClusterIP
	if *sourceContext != *destContext {
		svcType = corev1.ServiceTypeLoadBalancer
	}

	sourceCfg, err := buildConfigFromFlags(*sourceContext, *kubeconfig)
	if err != nil {
		log.WithError(err).Fatal("Error building kubeconfig")
	}

	sourceKubeClient, err := kubernetes.NewForConfig(sourceCfg)
	if err != nil {
		log.WithError(err).Fatal("Error building kubernetes clientset")
	}

	destCfg, err := buildConfigFromFlags(*destContext, *kubeconfig)
	if err != nil {
		log.WithError(err).Fatal("Error building kubeconfig")
	}

	destKubeClient, err := kubernetes.NewForConfig(destCfg)
	if err != nil {
		log.WithError(err).Fatal("Error building kubernetes clientset")
	}

	sourceClaimInfo := k8s.BuildClaimInfo(sourceKubeClient, sourceNamespace, source, *sourceReadOnly, false, svcType)
	destClaimInfo := k8s.BuildClaimInfo(destKubeClient, destNamespace, dest, false, *deleteExtraneousFromDest, svcType)

	log.Info("Both claims exist and bound, proceeding...")
	instance := randSeq(5)

	handleSigterm(sourceKubeClient, destKubeClient, instance, *sourceNamespace, *destNamespace)

	defer doCleanup(sourceKubeClient, instance, *sourceNamespace)
	defer doCleanup(destKubeClient, instance, *destNamespace)

	rsync.MigrateViaRsync(instance, sourceKubeClient, destKubeClient, sourceClaimInfo, destClaimInfo)
}

func handleSigterm(sourceKubeClient, destKubeClient *kubernetes.Clientset, instance string, sourceNamespace string, destNamespace string) {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		doCleanup(sourceKubeClient, instance, sourceNamespace)
		doCleanup(destKubeClient, instance, destNamespace)
		os.Exit(1)
	}()
}
