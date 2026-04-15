#!/usr/bin/env bash
set -euo pipefail

# Creates GCS resources for pv-migrate cloud integration tests.
# Idempotent — safe to run repeatedly.
#
# Optional env vars (non-sensitive, have defaults):
#   GCP_PROJECT                — GCP project ID (default: pv-migrate)
#
#   CLOUD_TEST_BUCKET          — bucket name (default: pv-migrate-test)
#   CLOUD_TEST_GCS_SA_NAME     — service account name (default: pv-migrate-test)
#   CLOUD_TEST_GCS_KEY_FILE    — where to write SA key (default: /tmp/pv-migrate-test-gcs.json)

GCP_PROJECT="${GCP_PROJECT:-pv-migrate}"

BUCKET="${CLOUD_TEST_BUCKET:-pv-migrate-test}"
SA_NAME="${CLOUD_TEST_GCS_SA_NAME:-pv-migrate-test}"
SA_EMAIL="${SA_NAME}@${GCP_PROJECT}.iam.gserviceaccount.com"
KEY_FILE="${CLOUD_TEST_GCS_KEY_FILE:-/tmp/pv-migrate-test-gcs.json}"

echo "=== Google Cloud Storage ==="

if gcloud storage buckets describe "gs://${BUCKET}" --project="${GCP_PROJECT}" &> /dev/null; then
  echo "Bucket gs://${BUCKET} already exists, skipping creation"
else
  gcloud storage buckets create "gs://${BUCKET}" \
    --project="${GCP_PROJECT}" \
    --location=us-east1 \
    --default-storage-class=STANDARD \
    --uniform-bucket-level-access
fi

cat << 'LIFECYCLE' > /tmp/gcs-lifecycle.json
{"rule": [{"action": {"type": "Delete"}, "condition": {"age": 1}}]}
LIFECYCLE
gcloud storage buckets update "gs://${BUCKET}" \
  --lifecycle-file=/tmp/gcs-lifecycle.json

if gcloud iam service-accounts describe "${SA_EMAIL}" --project="${GCP_PROJECT}" &> /dev/null; then
  echo "Service account ${SA_EMAIL} already exists, skipping creation"
else
  gcloud iam service-accounts create "${SA_NAME}" \
    --project="${GCP_PROJECT}" \
    --display-name="pv-migrate test"
fi

gcloud storage buckets add-iam-policy-binding "gs://${BUCKET}" \
  --member="serviceAccount:${SA_EMAIL}" \
  --role=roles/storage.objectAdmin

if [ -f "${KEY_FILE}" ]; then
  echo ""
  echo "WARNING: Key file ${KEY_FILE} already exists, skipping key creation."
  echo "To rotate, delete it and re-run:"
  echo "  rm ${KEY_FILE}"
else
  gcloud iam service-accounts keys create "${KEY_FILE}" \
    --iam-account="${SA_EMAIL}"
  echo "Service account key written to ${KEY_FILE}"
fi

cat << SUMMARY

GitHub Actions secrets:

  CLOUD_TEST_GCS_BUCKET              = ${BUCKET}
  CLOUD_TEST_GCS_SERVICE_ACCOUNT_JSON = (contents of ${KEY_FILE})

SUMMARY
