#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null && pwd )"

kubectl apply -f ${DIR}/case-2-same-ns-same-node.yaml
${DIR}/../pv-migrate \
  --source-namespace case-2-same-ns-same-node \
  --source pvc-a \
  --target-namespace case-2-same-ns-same-node \
  --target pvc-b
