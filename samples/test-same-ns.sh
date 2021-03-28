#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null && pwd )"

kubectl apply -f "${DIR}"/test-same-ns.yaml

"${DIR}"/../dist/pv-migrate_darwin_amd64/pv-migrate \
  --source-namespace pv-migrate-test-same-ns \
  --source pvc-a \
  --dest-namespace pv-migrate-test-same-ns \
  --dest pvc-b
