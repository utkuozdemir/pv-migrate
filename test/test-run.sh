#!/usr/bin/env bash
set -euo pipefail

SOURCE_KUBECONFIG=.kubeconfig-source.yaml
DEST_KUBECONFIG=.kubeconfig-dest.yaml

# Same namespace
pv-migrate \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  --source-namespace source \
  --source source \
  --dest-kubeconfig $SOURCE_KUBECONFIG \
  --dest-namespace source \
  --dest dest \
  --dest-delete-extraneous-files true

# Different namespace
pv-migrate \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  --source-namespace source \
  --source source \
  --dest-kubeconfig $SOURCE_KUBECONFIG \
  --dest-namespace dest \
  --dest dest \
  --dest-delete-extraneous-files true

# Different cluster
pv-migrate \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  --source-namespace source \
  --source source \
  --dest-kubeconfig $DEST_KUBECONFIG \
  --dest-namespace dest \
  --dest dest \
  --dest-delete-extraneous-files true
