#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null && pwd )"

kubectl apply -f ${DIR}/case-1-different-namespaces.yaml
${DIR}/../pv-migrate \
  --source-namespace case-1-different-namespaces-1 \
  --source pvc-a \
  --target-namespace case-1-different-namespaces-2 \
  --target pvc-b
