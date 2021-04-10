#!/usr/bin/env bash
set -euo pipefail

SOURCE_KUBECONFIG=.kubeconfig-source.yaml
DEST_KUBECONFIG=.kubeconfig-dest.yaml

pv-migrate \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  --source-namespace source \
  --source source \
  --dest-kubeconfig $DEST_KUBECONFIG \
  --dest-namespace dest \
  --dest dest \
  --dest-delete-extraneous-files true
