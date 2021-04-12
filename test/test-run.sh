#!/usr/bin/env bash
set -euo pipefail

SOURCE_KUBECONFIG=.kubeconfig-source.yaml
DEST_KUBECONFIG=.kubeconfig-dest.yaml

echo "Same namespace"
pv-migrate migrate \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  --source-namespace source \
  --dest-kubeconfig $SOURCE_KUBECONFIG \
  --dest-namespace source \
  --dest-delete-extraneous-files \
  source dest

echo "Different namespace"
pv-migrate migrate \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  --source-namespace source \
  --dest-kubeconfig $SOURCE_KUBECONFIG \
  --dest-namespace dest \
  --dest-delete-extraneous-files \
  source dest

echo "Different cluster"
pv-migrate migrate \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  -n source \
  --dest-kubeconfig $DEST_KUBECONFIG \
  -N dest \
  --dest-delete-extraneous-files \
  source dest

kubectl --kubeconfig $SOURCE_KUBECONFIG config set-context --current --namespace=source
kubectl --kubeconfig $DEST_KUBECONFIG config set-context --current --namespace=dest

echo "Different cluster - implicit ns"
pv-migrate m \
  -k $SOURCE_KUBECONFIG \
  -n source \
  -K $DEST_KUBECONFIG \
  -N dest \
  -d \
  source dest
