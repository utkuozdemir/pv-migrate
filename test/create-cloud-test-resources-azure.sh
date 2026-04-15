#!/usr/bin/env bash
set -euo pipefail

# Creates Azure Blob Storage resources for pv-migrate cloud integration tests.
# Idempotent — safe to run repeatedly.
#
# Optional env vars (non-sensitive, have defaults):
#   CLOUD_TEST_BUCKET                  — container name (default: pv-migrate-test)
#   CLOUD_TEST_AZURE_RG                — resource group (default: pv-migrate-test)
#   CLOUD_TEST_AZURE_STORAGE_ACCOUNT   — storage account name (default: pvmigratetest)
#   CLOUD_TEST_AZURE_LOCATION          — Azure region (default: eastus)

CONTAINER="${CLOUD_TEST_BUCKET:-pv-migrate-test}"
RG="${CLOUD_TEST_AZURE_RG:-pv-migrate-test}"
SA="${CLOUD_TEST_AZURE_STORAGE_ACCOUNT:-pvmigratetest}"
LOCATION="${CLOUD_TEST_AZURE_LOCATION:-eastus}"

echo "=== Azure Blob Storage ==="

az group create --name "${RG}" --location "${LOCATION}" -o none

if az storage account show --name "${SA}" --resource-group "${RG}" &> /dev/null; then
  echo "Storage account ${SA} already exists, skipping creation"
else
  az storage account create \
    --name "${SA}" \
    --resource-group "${RG}" \
    --location "${LOCATION}" \
    --sku Standard_LRS \
    --min-tls-version TLS1_2 \
    --allow-blob-public-access false \
    -o none
fi

az storage container create \
  --name "${CONTAINER}" \
  --account-name "${SA}" \
  --auth-mode login \
  -o none 2> /dev/null || true

az storage account management-policy create \
  --account-name "${SA}" \
  --resource-group "${RG}" \
  --policy '{
    "rules": [{
      "name": "cleanup",
      "enabled": true,
      "type": "Lifecycle",
      "definition": {
        "filters": {"blobTypes": ["blockBlob"]},
        "actions": {"baseBlob": {"delete": {"daysAfterModificationGreaterThan": 1}}}
      }
    }]
  }' -o none 2> /dev/null || true

STORAGE_KEY=$(az storage account keys list \
  --account-name "${SA}" \
  --resource-group "${RG}" \
  --query '[0].value' -o tsv)

cat << SUMMARY

GitHub Actions secrets:

  CLOUD_TEST_AZURE_STORAGE_ACCOUNT = ${SA}
  CLOUD_TEST_AZURE_STORAGE_KEY     = ${STORAGE_KEY}
  CLOUD_TEST_AZURE_CONTAINER       = ${CONTAINER}

SUMMARY
