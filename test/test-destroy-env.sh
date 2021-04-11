#!/usr/bin/env bash
set -euo pipefail

terraform -chdir=terraform/ destroy -auto-approve

GCP_PROJECT=pv-migrate

gcloud --project $GCP_PROJECT compute disks list --format="value(zone.basename(), name)" \
  | awk '{print "--zone " $1 " " $2}' \
  | xargs -L 1 gcloud --project pv-migrate compute disks delete --quiet
