#!/usr/bin/env bash
set -euo pipefail

SOURCE_KUBECONFIG=.kubeconfig-source.yaml
DEST_KUBECONFIG=.kubeconfig-dest.yaml

echo "----------------------------------"
echo "Same namespace - legacy migrate subcommand"
pv-migrate migrate \
  --ignore-mounted \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  --source-namespace pv-migrate-test-1 \
  --dest-kubeconfig $SOURCE_KUBECONFIG \
  --dest-namespace pv-migrate-test-1 \
  --dest-delete-extraneous-files \
  pv-migrate-test-source-1 \
  pv-migrate-test-dest-1

echo "----------------------------------"
echo "Same namespace"
pv-migrate \
  --ignore-mounted \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  --source-namespace pv-migrate-test-1 \
  --source pv-migrate-test-source-1 \
  --dest-kubeconfig $SOURCE_KUBECONFIG \
  --dest-namespace pv-migrate-test-1 \
  --dest-delete-extraneous-files \
  --dest pv-migrate-test-dest-1

echo "----------------------------------"
echo "Different namespace"
pv-migrate \
  --ignore-mounted \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  --source-namespace pv-migrate-test-1 \
  --source pv-migrate-test-source-1 \
  --dest-kubeconfig $SOURCE_KUBECONFIG \
  --dest-namespace pv-migrate-test-2 \
  --dest-delete-extraneous-files \
  --dest pv-migrate-test-dest-2

echo "----------------------------------"
echo "Different cluster"
pv-migrate \
  --log-level info \
  --log-format fancy \
  migrate \
  --ignore-mounted \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  -n pv-migrate-test-1 \
  --dest-kubeconfig $DEST_KUBECONFIG \
  -N pv-migrate-test-1 \
  --dest-delete-extraneous-files \
  pv-migrate-test-source-1 pv-migrate-test-dest-1

echo "----------------------------------"
echo "Different cluster - local strategy"
pv-migrate \
  --strategies local \
  --ignore-mounted \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  -n pv-migrate-test-1 \
  --source pv-migrate-test-source-1 \
  --dest-kubeconfig $DEST_KUBECONFIG \
  -N pv-migrate-test-1 \
  --dest-delete-extraneous-files \
  --dest pv-migrate-test-dest-1
echo "----------------------------------"
echo "Different cluster - NodePort strategy"
pv-migrate \
  --strategies nodeport \
  --ignore-mounted \
  --source-kubeconfig $SOURCE_KUBECONFIG \
  -n pv-migrate-test-1 \
  --source pv-migrate-test-source-1 \
  --dest-kubeconfig $DEST_KUBECONFIG \
  -N pv-migrate-test-1 \
  --dest-delete-extraneous-files \
  --dest pv-migrate-test-dest-1