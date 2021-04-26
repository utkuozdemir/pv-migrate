package integrationtest

import (
	"fmt"
	"github.com/utkuozdemir/pv-migrate/internal/util"
)

const (
	resourcePrefix = "pv-migrate-test-"
)

func generateTestResourceName() string {
	return fmt.Sprintf("%s%s", resourcePrefix, util.RandomHexadecimalString(5))
}
