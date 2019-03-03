#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

dep ensure
GOOS=linux go build -a -installsuffix cgo -o bin/linux/pv-migrate
GOOS=windows go build -a -installsuffix cgo -o bin/windows/pv-migrate
GOOS=darwin go build -a -installsuffix cgo -o bin/darwin/pv-migrate

tar -czvf pv-migrate-linux.tar.gz bin/linux/pv-migrate
tar -czvf pv-migrate-windows.tar.gz bin/windows/pv-migrate
tar -czvf pv-migrate-darwin.tar.gz bin/darwin/pv-migrate
