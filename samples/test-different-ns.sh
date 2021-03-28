#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null && pwd )"

kubectl apply -f "${DIR}"/test-different-ns.yaml

#"${DIR}"/../dist/pv-migrate_darwin_amd64/pv-migrate \
#  --source-namespace pv-migrate-test-different-ns-1 \
#  --source pvc-a \
#  --dest-namespace pv-migrate-test-different-ns-2 \
#  --dest pvc-b
