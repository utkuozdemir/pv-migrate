#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null && pwd )"

kubectl apply -f ${DIR}/case-3-accessmany-source.yaml
${DIR}/../pv-migrate \
  --source-namespace case-3-accessmany-source \
  --source pvc-a \
  --target-namespace case-3-accessmany-source \
  --target pvc-b
