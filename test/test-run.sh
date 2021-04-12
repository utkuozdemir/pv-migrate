#!/usr/bin/env bash
set -euo pipefail

SOURCE_KUBECONFIG=.kubeconfig-source.yaml
DEST_KUBECONFIG=.kubeconfig-dest.yaml

# Same namespace
pv-migrate migrate \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  --source-namespace source \
  --dest-kubeconfig $SOURCE_KUBECONFIG \
  --dest-namespace source \
  --dest-delete-extraneous-files true \
  source dest

# Different namespace
pv-migrate migrate \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  --source-namespace source \
  --dest-kubeconfig $SOURCE_KUBECONFIG \
  --dest-namespace dest \
  --dest-delete-extraneous-files true \
  source dest

# Different cluster
pv-migrate \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  --source-namespace source \
  --dest-kubeconfig $DEST_KUBECONFIG \
  --dest-namespace dest \
  --dest-delete-extraneous-files true \
  source dest
