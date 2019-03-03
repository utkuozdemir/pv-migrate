#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null && pwd )"

kubectl apply -f ${DIR}/test-different-ns.yaml
${DIR}/../pv-migrate \
  --source-namespace test-different-ns-1 \
  --source pvc-a \
  --dest-namespace test-different-ns-2 \
  --dest pvc-b
