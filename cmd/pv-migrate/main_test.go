package main

import (
	"github.com/stretchr/testify/assert"
	"github.com/utkuozdemir/pv-migrate/internal/app"
	"io/ioutil"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	beforeTests()
	code := m.Run()
	afterTests()
	os.Exit(code)
}

func TestSameNS(t *testing.T) {
	cliApp := app.Build()
	args := []string{
		os.Args[0], "migrate",
		"--source-kubeconfig", testContext.kubeconfig,
		"--source-namespace", "aaa",
		"--dest-kubeconfig", testContext.kubeconfig,
		"--dest-namespace", "aaa",
		"aaa", "bbb",
	}

	_, _, err := execInFirstPodWithPrefix("aaa", "aaa",
		[]string{"sh", "-c", "echo -n aaaaa > /volume/file.txt"})
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.NoError(t, err)

	stdout, stderr, err := execInFirstPodWithPrefix("aaa", "bbb",
		[]string{"cat", "/volume/file.txt"})
	assert.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Equal(t, "aaaaa", stdout)
}

func TestDifferentNS(t *testing.T) {
	cliApp := app.Build()

	args := []string{
		os.Args[0], "migrate",
		"--source-kubeconfig", testContext.kubeconfig,
		"--source-namespace", "aaa",
		"--dest-kubeconfig", testContext.kubeconfig,
		"--dest-namespace", "bbb",
		"aaa", "bbb",
	}

	_, _, err := execInFirstPodWithPrefix("aaa", "aaa",
		[]string{"sh", "-c", "echo -n DATA > /volume/file.txt"})
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.NoError(t, err)

	stdout, stderr, err := execInFirstPodWithPrefix("bbb", "bbb",
		[]string{"cat", "/volume/file.txt"})
	assert.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Equal(t, "DATA", stdout)
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
		os.Args[0], "migrate",
		"--source-kubeconfig", testContext.kubeconfig,
		"--source-namespace", "aaa",
		"--dest-kubeconfig", kubeconfigCopy,
		"--dest-namespace", "ccc",
		"aaa", "ccc",
	}

	_, _, err := execInFirstPodWithPrefix("aaa", "aaa",
		[]string{"sh", "-c", "echo -n DATA > /volume/file.txt"})
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.NoError(t, err)

	stdout, stderr, err := execInFirstPodWithPrefix("ccc", "ccc",
		[]string{"cat", "/volume/file.txt"})
	assert.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Equal(t, "DATA", stdout)
}
