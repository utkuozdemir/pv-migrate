package integrationtest

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/utkuozdemir/pv-migrate/internal/app"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"testing"
)

const (
	flagPrefix                = "--"
	sourceKubeconfigParamKey  = flagPrefix + app.FlagSourceKubeconfig
	sourceNsParamKey          = flagPrefix + app.FlagSourceNamespace
	destKubeconfigParamKey    = flagPrefix + app.FlagDestKubeconfig
	destNsParamKey            = flagPrefix + app.FlagDestNamespace
	ignoreMountedFlag         = flagPrefix + app.FlagIgnoreMounted
	noChownFlag               = flagPrefix + app.FlagNoChown
	deleteExtraneousFilesFlag = flagPrefix + app.FlagDestDeleteExtraneousFiles
	migrateCommand            = app.CommandMigrate

	dataFileUid         = "12345"
	dataFileGid         = "54321"
	dataFilePath        = "/volume/file.txt"
	extraDataFilePath   = "/volume/extra_file.txt"
	generateDataContent = "DATA"

	noKindEnvVar = "PV_MIGRATE_TEST_NO_KIND"
)

var (
	ctx                      *pvMigrateTestContext
	generateDataShellCommand = []string{"sh", "-c", fmt.Sprintf("echo -n %s > %s && chown %s:%s %s",
		generateDataContent, dataFilePath, dataFileUid, dataFileGid, dataFilePath)}
	generateExtraDataShellCommand = []string{"sh", "-c", fmt.Sprintf("echo -n %s > %s",
		generateDataContent, extraDataFilePath)}
	printDataContentShellCommand       = []string{"cat", dataFilePath}
	printDataUidGidContentShellCommand = []string{"sh", "-c",
		fmt.Sprintf("stat -c '%%u' %s && stat -c '%%g' %s && cat %s", dataFilePath, dataFilePath, dataFilePath)}
	checkExtraDataShellCommand = []string{"ls", extraDataFilePath}
)

func TestMain(m *testing.M) {
	ctx = prepareTestContext()
	code := m.Run()
	finalizeTestContext(ctx)
	os.Exit(code)
}

func TestSameNS(t *testing.T) {
	sourceNs, source, err := randomTestNamespaceWithRandomBoundPVC()
	assert.NoError(t, err)
	dest, err := testNamespaceWithRandomBoundPVC(sourceNs)
	assert.NoError(t, err)
	destNs := sourceNs

	cliApp := app.New("", "")
	args := []string{
		os.Args[0], migrateCommand,
		sourceKubeconfigParamKey, ctx.kubeconfig,
		sourceNsParamKey, sourceNs,
		destKubeconfigParamKey, ctx.kubeconfig,
		destNsParamKey, destNs,
		source, dest,
	}
	defer func() {
		err = ensureNamespaceIsDeleted(ctx.kubeClient, sourceNs)
		assert.NoError(t, err)
	}()

	_, _, err = execInPodWithPVC(ctx.kubeClient, ctx.config, sourceNs, source, generateDataShellCommand)
	assert.NoError(t, err)

	// generate extra file
	_, _, err = execInPodWithPVC(ctx.kubeClient, ctx.config, destNs, dest, generateExtraDataShellCommand)
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.NoError(t, err)

	stdout, stderr, err := execInPodWithPVC(ctx.kubeClient, ctx.config, destNs, dest, printDataUidGidContentShellCommand)
	assert.NoError(t, err)
	assert.Empty(t, stderr)

	parts := strings.Split(stdout, "\n")
	uid := parts[0]
	gid := parts[1]
	content := parts[2]

	assert.Equal(t, dataFileUid, uid)
	assert.Equal(t, dataFileGid, gid)
	assert.Equal(t, generateDataContent, content)

	_, _, err = execInPodWithPVC(ctx.kubeClient, ctx.config, destNs, dest, checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestNoChown(t *testing.T) {
	sourceNs, source, err := randomTestNamespaceWithRandomBoundPVC()
	assert.NoError(t, err)
	dest, err := testNamespaceWithRandomBoundPVC(sourceNs)
	assert.NoError(t, err)
	destNs := sourceNs

	cliApp := app.New("", "")
	args := []string{
		os.Args[0], migrateCommand,
		sourceKubeconfigParamKey, ctx.kubeconfig,
		sourceNsParamKey, sourceNs,
		destKubeconfigParamKey, ctx.kubeconfig,
		destNsParamKey, destNs,
		noChownFlag,
		source, dest,
	}
	defer func() {
		err = ensureNamespaceIsDeleted(ctx.kubeClient, sourceNs)
		assert.NoError(t, err)
	}()

	_, _, err = execInPodWithPVC(ctx.kubeClient, ctx.config, sourceNs, source, generateDataShellCommand)
	assert.NoError(t, err)

	// generate extra file
	_, _, err = execInPodWithPVC(ctx.kubeClient, ctx.config, destNs, dest, generateExtraDataShellCommand)
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.NoError(t, err)

	stdout, stderr, err := execInPodWithPVC(ctx.kubeClient, ctx.config, destNs, dest, printDataUidGidContentShellCommand)
	assert.NoError(t, err)
	assert.Empty(t, stderr)

	parts := strings.Split(stdout, "\n")
	uid := parts[0]
	gid := parts[1]
	content := parts[2]

	assert.Equal(t, "0", uid)
	assert.Equal(t, "0", gid)
	assert.Equal(t, generateDataContent, content)

	_, _, err = execInPodWithPVC(ctx.kubeClient, ctx.config, destNs, dest, checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestSameNSDeleteExtraneousFiles(t *testing.T) {
	sourceNs, source, err := randomTestNamespaceWithRandomBoundPVC()
	assert.NoError(t, err)
	dest, err := testNamespaceWithRandomBoundPVC(sourceNs)
	assert.NoError(t, err)
	destNs := sourceNs

	cliApp := app.New("", "")
	args := []string{
		os.Args[0], migrateCommand,
		sourceKubeconfigParamKey, ctx.kubeconfig,
		sourceNsParamKey, sourceNs,
		destKubeconfigParamKey, ctx.kubeconfig,
		destNsParamKey, destNs,
		deleteExtraneousFilesFlag,
		source, dest,
	}
	defer func() {
		err = ensureNamespaceIsDeleted(ctx.kubeClient, sourceNs)
		assert.NoError(t, err)
	}()

	_, _, err = execInPodWithPVC(ctx.kubeClient, ctx.config, sourceNs, source, generateDataShellCommand)
	assert.NoError(t, err)

	// generate extra file
	_, _, err = execInPodWithPVC(ctx.kubeClient, ctx.config, destNs, dest, generateExtraDataShellCommand)
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.NoError(t, err)

	stdout, stderr, err := execInPodWithPVC(ctx.kubeClient, ctx.config, destNs, dest, printDataContentShellCommand)
	assert.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Equal(t, generateDataContent, stdout)

	_, stderr, err = execInPodWithPVC(ctx.kubeClient, ctx.config, destNs, dest, checkExtraDataShellCommand)
	assert.Error(t, err)
	assert.Contains(t, stderr, "No such file or directory")
}

func TestMountedError(t *testing.T) {
	sourceNs, source, err := randomTestNamespaceWithRandomBoundPVC()
	assert.NoError(t, err)
	dest, err := testNamespaceWithRandomBoundPVC(sourceNs)
	assert.NoError(t, err)
	destNs := sourceNs
	cliApp := app.New("", "")
	args := []string{
		os.Args[0], migrateCommand,
		sourceKubeconfigParamKey, ctx.kubeconfig,
		sourceNsParamKey, sourceNs,
		destKubeconfigParamKey, ctx.kubeconfig,
		destNsParamKey, destNs,
		source, dest,
	}
	defer func() {
		err = ensureNamespaceIsDeleted(ctx.kubeClient, sourceNs)
		assert.NoError(t, err)
	}()

	_, _, err = execInPodWithPVC(ctx.kubeClient, ctx.config, sourceNs, source, generateDataShellCommand)
	assert.NoError(t, err)

	_, err = startPodWithPVCMount(ctx.kubeClient, sourceNs, source)
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.Error(t, err)
}

func TestIgnoreMounted(t *testing.T) {
	sourceNs, source, err := randomTestNamespaceWithRandomBoundPVC()
	assert.NoError(t, err)
	dest, err := testNamespaceWithRandomBoundPVC(sourceNs)
	assert.NoError(t, err)
	destNs := sourceNs
	cliApp := app.New("", "")
	args := []string{
		os.Args[0], migrateCommand,
		sourceKubeconfigParamKey, ctx.kubeconfig,
		sourceNsParamKey, sourceNs,
		destKubeconfigParamKey, ctx.kubeconfig,
		destNsParamKey, destNs,
		ignoreMountedFlag,
		source, dest,
	}
	defer func() {
		err = ensureNamespaceIsDeleted(ctx.kubeClient, sourceNs)
		assert.NoError(t, err)
	}()

	_, _, err = execInPodWithPVC(ctx.kubeClient, ctx.config, sourceNs, source, generateDataShellCommand)
	assert.NoError(t, err)

	_, err = startPodWithPVCMount(ctx.kubeClient, sourceNs, source)
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.NoError(t, err)

	stdout, stderr, err := execInPodWithPVC(ctx.kubeClient, ctx.config, destNs, dest, printDataContentShellCommand)
	assert.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Equal(t, generateDataContent, stdout)
}

func TestDifferentNS(t *testing.T) {
	sourceNs, source, err := randomTestNamespaceWithRandomBoundPVC()
	assert.NoError(t, err)
	destNs, dest, err := randomTestNamespaceWithRandomBoundPVC()
	assert.NoError(t, err)
	cliApp := app.New("", "")

	args := []string{
		os.Args[0], migrateCommand,
		sourceKubeconfigParamKey, ctx.kubeconfig,
		sourceNsParamKey, sourceNs,
		destKubeconfigParamKey, ctx.kubeconfig,
		destNsParamKey, destNs,
		source, dest,
	}
	defer func() {
		err = ensureNamespaceIsDeleted(ctx.kubeClient, sourceNs)
		assert.NoError(t, err)
		err = ensureNamespaceIsDeleted(ctx.kubeClient, destNs)
		assert.NoError(t, err)
	}()

	_, _, err = execInPodWithPVC(ctx.kubeClient, ctx.config, sourceNs, source, generateDataShellCommand)
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.NoError(t, err)

	stdout, stderr, err := execInPodWithPVC(ctx.kubeClient, ctx.config, destNs, dest, printDataContentShellCommand)
	assert.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Equal(t, generateDataContent, stdout)
}

// TestDifferentCluster will trick the application to "think" that source and dest are in 2 different clusters
// while actually both of them being in the same "kind cluster".
func TestDifferentCluster(t *testing.T) {
	sourceNs, source, err := randomTestNamespaceWithRandomBoundPVC()
	assert.NoError(t, err)
	destNs, dest, err := randomTestNamespaceWithRandomBoundPVC()
	assert.NoError(t, err)
	kubeconfigBytes, _ := ioutil.ReadFile(ctx.kubeconfig)
	kubeconfigCopyFile, _ := ioutil.TempFile("", "pv-migrate-kind-config-*.yaml")
	kubeconfigCopy := kubeconfigCopyFile.Name()
	_ = ioutil.WriteFile(kubeconfigCopy, kubeconfigBytes, 0600)
	defer func() { _ = os.Remove(kubeconfigCopy) }()

	cliApp := app.New("", "")

	args := []string{
		os.Args[0], migrateCommand,
		sourceKubeconfigParamKey, ctx.kubeconfig,
		sourceNsParamKey, sourceNs,
		destKubeconfigParamKey, kubeconfigCopy,
		destNsParamKey, destNs,
		source, dest,
	}
	defer func() {
		err = ensureNamespaceIsDeleted(ctx.kubeClient, sourceNs)
		assert.NoError(t, err)
		err = ensureNamespaceIsDeleted(ctx.kubeClient, destNs)
		assert.NoError(t, err)
	}()

	_, _, err = execInPodWithPVC(ctx.kubeClient, ctx.config, sourceNs, source, generateDataShellCommand)
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.NoError(t, err)

	stdout, stderr, err := execInPodWithPVC(ctx.kubeClient, ctx.config, destNs, dest, printDataContentShellCommand)
	assert.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Equal(t, generateDataContent, stdout)
}

func randomTestNamespaceWithRandomBoundPVC() (string, string, error) {
	ns := generateTestResourceName()
	pvc, err := testNamespaceWithRandomBoundPVC(ns)
	return ns, pvc, err
}

func testNamespaceWithRandomBoundPVC(namespace string) (string, error) {
	c := ctx.kubeClient
	_, err := ensureNamespaceExists(c, namespace)
	if err != nil {
		return "", err
	}

	pvc, err := ensurePVCExistsAndBound(c, namespace, generateTestResourceName())
	if err != nil {
		return "", err
	}

	return pvc.Name, nil
}

func useKind() bool {
	parsed, err := strconv.ParseBool(os.Getenv(noKindEnvVar))
	if err != nil {
		return true
	}

	return !parsed
}
