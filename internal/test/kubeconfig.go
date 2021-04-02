package test

import (
	_ "embed"
	"io/ioutil"
	"os"
)

//go:embed kubeconfig.yaml
var kubeconfig string

func PrepareKubeconfig() string {
	testConfig, _ := ioutil.TempFile("", "pv-migrate-testconfig-*.yaml")
	_, _ = testConfig.WriteString(kubeconfig)
	return testConfig.Name()
}

func DeleteKubeconfig(path string) {
	_ = os.Remove(path)
}
