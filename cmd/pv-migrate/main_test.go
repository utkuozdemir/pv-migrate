package main

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/utkuozdemir/pv-migrate/internal/app"
	"io/ioutil"
	"os"
	"testing"
)

const (
	flagPrefix               = "--"
	sourceKubeconfigParamKey = flagPrefix + app.FlagSourceKubeconfig
	sourceNsParamKey         = flagPrefix + app.FlagSourceNamespace
	destKubeconfigParamKey   = flagPrefix + app.FlagDestKubeconfig
	destNsParamKey           = flagPrefix + app.FlagDestNamespace
	ignoreMountedFlag        = flagPrefix + app.FlagIgnoreMounted
	migrateCommand           = app.CommandMigrate

	testFilePath        = "/volume/file.txt"
	generateDataContent = "DATA"
)

var (
	generateDataShellCommand = []string{"sh", "-c", fmt.Sprintf("echo -n %s > %s", generateDataContent, testFilePath)}
	printDataShellCommand    = []string{"cat", testFilePath}
	removeDataShellCommand   = []string{"rm", "-rf", testFilePath}
)

func TestMain(m *testing.M) {
	beforeTests()
	code := m.Run()
	afterTests()
	os.Exit(code)
}

func TestSameNSNoIgnoreMounted(t *testing.T) {
	cliApp := app.New("", "")
	args := []string{
		os.Args[0], migrateCommand,
		sourceKubeconfigParamKey, testContext.kubeconfig,
		sourceNsParamKey, "aaa",
		destKubeconfigParamKey, testContext.kubeconfig,
		destNsParamKey, "aaa",
		"aaa", "bbb",
	}
	defer func() {
		_, _, err := execInFirstPodWithPrefix("aaa", "bbb", removeDataShellCommand)
		assert.NoError(t, err)
	}()

	_, _, err := execInFirstPodWithPrefix("aaa", "aaa", generateDataShellCommand)
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.Error(t, err)
}

func TestSameNS(t *testing.T) {
	cliApp := app.New("", "")
	args := []string{
		os.Args[0], migrateCommand,
		sourceKubeconfigParamKey, testContext.kubeconfig,
		sourceNsParamKey, "aaa",
		destKubeconfigParamKey, testContext.kubeconfig,
		destNsParamKey, "aaa",
		ignoreMountedFlag,
		"aaa", "bbb",
	}
	defer func() {
		_, _, err := execInFirstPodWithPrefix("aaa", "bbb", removeDataShellCommand)
		assert.NoError(t, err)
	}()

	_, _, err := execInFirstPodWithPrefix("aaa", "aaa", generateDataShellCommand)
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.NoError(t, err)

	stdout, stderr, err := execInFirstPodWithPrefix("aaa", "bbb", printDataShellCommand)
	assert.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Equal(t, generateDataContent, stdout)
}

func TestDifferentNS(t *testing.T) {
	cliApp := app.New("", "")

	args := []string{
		os.Args[0], migrateCommand,
		sourceKubeconfigParamKey, testContext.kubeconfig,
		sourceNsParamKey, "aaa",
		destKubeconfigParamKey, testContext.kubeconfig,
		destNsParamKey, "bbb",
		ignoreMountedFlag,
		"aaa", "bbb",
	}
	defer func() {
		_, _, err := execInFirstPodWithPrefix("bbb", "bbb", removeDataShellCommand)
		assert.NoError(t, err)
	}()

	_, _, err := execInFirstPodWithPrefix("aaa", "aaa", generateDataShellCommand)
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.NoError(t, err)

	stdout, stderr, err := execInFirstPodWithPrefix("bbb", "bbb", printDataShellCommand)
	assert.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Equal(t, generateDataContent, stdout)
}

// TestDifferentCluster will trick the application to "think" that source and dest are in 2 different clusters
// while actually both of them being in the same "kind cluster".
func TestDifferentCluster(t *testing.T) {
	kubeconfigBytes, _ := ioutil.ReadFile(testContext.kubeconfig)
	kubeconfigCopyFile, _ := ioutil.TempFile("", "pv-migrate-kind-config-*.yaml")
	kubeconfigCopy := kubeconfigCopyFile.Name()
	_ = ioutil.WriteFile(kubeconfigCopy, kubeconfigBytes, 0600)
	defer func() { _ = os.Remove(kubeconfigCopy) }()

	cliApp := app.New("", "")

	args := []string{
		os.Args[0], migrateCommand,
		sourceKubeconfigParamKey, testContext.kubeconfig,
		sourceNsParamKey, "aaa",
		destKubeconfigParamKey, kubeconfigCopy,
		destNsParamKey, "ccc",
		ignoreMountedFlag,
		"aaa", "ccc",
	}
	defer func() {
		_, _, err := execInFirstPodWithPrefix("ccc", "ccc", removeDataShellCommand)
		assert.NoError(t, err)
	}()

	_, _, err := execInFirstPodWithPrefix("aaa", "aaa", generateDataShellCommand)
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.NoError(t, err)

	stdout, stderr, err := execInFirstPodWithPrefix("ccc", "ccc", printDataShellCommand)
	assert.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Equal(t, generateDataContent, stdout)
}
