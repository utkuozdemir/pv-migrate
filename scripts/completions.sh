#!/bin/sh
set -e
rm -rf completions
mkdir completions
for sh in bash zsh fish; do
	go run cmd/pv-migrate/main.go completion "$sh" >"completions/pv-migrate.$sh"
done
