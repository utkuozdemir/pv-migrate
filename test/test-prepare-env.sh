#!/usr/bin/env bash
set -euo pipefail

GCP_PROJECT=pv-migrate
GCP_ZONE=europe-west3-a

SOURCE_KUBECONFIG=.kubeconfig-source.yaml
DEST_KUBECONFIG=.kubeconfig-dest.yaml

terraform -chdir=terraform/ apply -auto-approve

KUBECONFIG=$SOURCE_KUBECONFIG gcloud \
  --project $GCP_PROJECT \
  container clusters get-credentials \
  --zone $GCP_ZONE pv-migrate-test-1

KUBECONFIG=$DEST_KUBECONFIG gcloud \
  --project $GCP_PROJECT \
  container clusters get-credentials \
  --zone $GCP_ZONE pv-migrate-test-2

# Source
kubectl --kubeconfig $SOURCE_KUBECONFIG apply -f k8s/source-ns.yaml
kubectl --kubeconfig $SOURCE_KUBECONFIG --namespace source apply -f k8s/source.yaml

# Dest: same namespace
kubectl --kubeconfig $SOURCE_KUBECONFIG --namespace source apply -f k8s/dest.yaml

# Dest: different namespace
kubectl --kubeconfig $SOURCE_KUBECONFIG apply -f k8s/dest-ns.yaml
kubectl --kubeconfig $SOURCE_KUBECONFIG --namespace dest apply -f k8s/dest.yaml

# Dest: different cluster
kubectl --kubeconfig $DEST_KUBECONFIG apply -f k8s/dest-ns.yaml
kubectl --kubeconfig $DEST_KUBECONFIG --namespace dest apply -f k8s/dest.yaml
