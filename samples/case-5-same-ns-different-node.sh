#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null && pwd )"

kubectl apply -f ${DIR}/case-5-same-ns-different-node.yaml
${DIR}/../pv-migrate \
  --source-namespace case-5-same-ns-different-node \
  --source pvc-a \
  --target-namespace case-5-same-ns-different-node \
  --target pvc-b
