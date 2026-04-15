#!/usr/bin/env bash
set -euo pipefail

# Creates AWS S3 resources for pv-migrate cloud integration tests.
# Idempotent — safe to run repeatedly.
#
# Optional env vars (non-sensitive, have defaults):
#   CLOUD_TEST_BUCKET          — bucket name (default: pv-migrate-test)
#   CLOUD_TEST_AWS_REGION      — S3 region (default: us-east-1)
#   CLOUD_TEST_AWS_IAM_USER    — IAM user name (default: pv-migrate-test)

export AWS_PAGER=""

BUCKET="${CLOUD_TEST_BUCKET:-pv-migrate-test}"
REGION="${CLOUD_TEST_AWS_REGION:-us-east-1}"
IAM_USER="${CLOUD_TEST_AWS_IAM_USER:-pv-migrate-test}"

echo "=== AWS S3 ==="

if aws s3api head-bucket --bucket "${BUCKET}" 2> /dev/null; then
  echo "Bucket s3://${BUCKET} already exists, skipping creation"
else
  aws s3 mb "s3://${BUCKET}" --region "${REGION}"
fi

aws s3api put-bucket-lifecycle-configuration \
  --bucket "${BUCKET}" \
  --lifecycle-configuration '{
    "Rules": [{"ID": "cleanup", "Status": "Enabled", "Filter": {}, "Expiration": {"Days": 1}}]
  }'

if aws iam get-user --user-name "${IAM_USER}" &> /dev/null; then
  echo "IAM user ${IAM_USER} already exists, skipping creation"
else
  aws iam create-user --user-name "${IAM_USER}"
fi

aws iam put-user-policy \
  --user-name "${IAM_USER}" \
  --policy-name "${IAM_USER}-s3" \
  --policy-document "{
    \"Version\": \"2012-10-17\",
    \"Statement\": [{
      \"Effect\": \"Allow\",
      \"Action\": [\"s3:GetObject\", \"s3:PutObject\", \"s3:DeleteObject\", \"s3:ListBucket\"],
      \"Resource\": [
        \"arn:aws:s3:::${BUCKET}\",
        \"arn:aws:s3:::${BUCKET}/*\"
      ]
    }]
  }"

EXISTING_KEYS=$(aws iam list-access-keys --user-name "${IAM_USER}" --query 'AccessKeyMetadata[*].AccessKeyId' --output text)
if [ -n "${EXISTING_KEYS}" ]; then
  echo ""
  echo "WARNING: IAM user ${IAM_USER} already has access key(s): ${EXISTING_KEYS}"
  echo "Skipping key creation. To rotate, delete existing keys and re-run:"
  for KEY_ID in ${EXISTING_KEYS}; do
    echo "  aws iam delete-access-key --user-name ${IAM_USER} --access-key-id ${KEY_ID}"
  done
else
  echo ""
  echo "Creating access key for ${IAM_USER}:"
  aws iam create-access-key --user-name "${IAM_USER}"
fi

cat << SUMMARY

GitHub Actions secrets:

  CLOUD_TEST_S3_BUCKET     = ${BUCKET}
  CLOUD_TEST_S3_REGION     = ${REGION}
  CLOUD_TEST_S3_ACCESS_KEY = (from access key output above)
  CLOUD_TEST_S3_SECRET_KEY = (from access key output above)

SUMMARY
