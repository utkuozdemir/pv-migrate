package main

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/utkuozdemir/pv-migrate/internal/app"
	"io/ioutil"
	"os"
	"testing"
)

const sourceKubeconfigParamKey = "--source-kubeconfig"
const sourceNsParamKey = "--source-namespace"
const destKubeconfigParamKey = "--dest-kubeconfig"
const destNsParamKey = "--dest-namespace"
const testFilePath = "/volume/file.txt"
const migrateCommand = "migrate"
const generateDataContent = "DATA"

var generateDataShellCommand = []string{"sh", "-c", fmt.Sprintf("echo -n %s > %s", generateDataContent, testFilePath)}
var printDataShellCommand = []string{"cat", testFilePath}

func TestMain(m *testing.M) {
	beforeTests()
	code := m.Run()
	afterTests()
	os.Exit(code)
}

func TestSameNS(t *testing.T) {
	cliApp := app.Build()
	args := []string{
		os.Args[0], migrateCommand,
		sourceKubeconfigParamKey, testContext.kubeconfig,
		sourceNsParamKey, "aaa",
		destKubeconfigParamKey, testContext.kubeconfig,
		destNsParamKey, "aaa",
		"aaa", "bbb",
	}

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
	cliApp := app.Build()

	args := []string{
		os.Args[0], migrateCommand,
		sourceKubeconfigParamKey, testContext.kubeconfig,
		sourceNsParamKey, "aaa",
		destKubeconfigParamKey, testContext.kubeconfig,
		destNsParamKey, "bbb",
		"aaa", "bbb",
	}

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

	cliApp := app.Build()

	args := []string{
		os.Args[0], migrateCommand,
		sourceKubeconfigParamKey, testContext.kubeconfig,
		sourceNsParamKey, "aaa",
		destKubeconfigParamKey, kubeconfigCopy,
		destNsParamKey, "ccc",
		"aaa", "ccc",
	}

	_, _, err := execInFirstPodWithPrefix("aaa", "aaa", generateDataShellCommand)
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.NoError(t, err)

	stdout, stderr, err := execInFirstPodWithPrefix("ccc", "ccc", printDataShellCommand)
	assert.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Equal(t, generateDataContent, stdout)
}
