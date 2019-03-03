#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null && pwd )"

kubectl apply -f ${DIR}/test-same-ns.yaml
${DIR}/../pv-migrate \
  --source-namespace test-same-ns \
  --source pvc-a \
  --dest-namespace test-same-ns \
  --dest pvc-b
