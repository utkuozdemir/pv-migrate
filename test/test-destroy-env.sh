#!/usr/bin/env bash
set -euo pipefail

terraform -chdir=terraform/ destroy -auto-approve
