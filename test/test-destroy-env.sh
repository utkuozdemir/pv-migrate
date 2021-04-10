#!/usr/bin/env bash
set -euo pipefail

terraform -chdir=terraform/ destroy -auto-approve

# todo: delete persistent disks: https://console.cloud.google.com/compute/disks?project=pv-migrate
