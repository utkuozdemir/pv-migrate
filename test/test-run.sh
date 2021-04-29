#!/usr/bin/env bash
set -euo pipefail

SOURCE_KUBECONFIG=.kubeconfig-source.yaml
DEST_KUBECONFIG=.kubeconfig-dest.yaml

echo "Same namespace"
pv-migrate migrate \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  --source-namespace pv-migrate-test-1 \
  --dest-kubeconfig $SOURCE_KUBECONFIG \
  --dest-namespace pv-migrate-test-1 \
  --dest-delete-extraneous-files \
  pv-migrate-test-source-1 pv-migrate-test-dest-1

echo "Different namespace"
pv-migrate migrate \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  --source-namespace pv-migrate-test-1 \
  --dest-kubeconfig $SOURCE_KUBECONFIG \
  --dest-namespace pv-migrate-test-2 \
  --dest-delete-extraneous-files \
  pv-migrate-test-source-1 pv-migrate-test-dest-2

echo "Different cluster"
pv-migrate migrate \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  -n pv-migrate-test-1 \
  --dest-kubeconfig $DEST_KUBECONFIG \
  -N pv-migrate-test-1 \
  --dest-delete-extraneous-files \
  pv-migrate-test-source-1 pv-migrate-test-dest-1
