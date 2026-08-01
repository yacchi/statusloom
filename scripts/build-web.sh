#!/usr/bin/env bash
# Builds the configurator frontend (apps/configurator) and copies the
# resulting Vite build into internal/webconfig/dist so `go build`/`go run`
# embed the real UI.
#
# The output needs no clean-up before committing: everything this script writes
# is excluded by internal/webconfig/dist/.gitignore, and a checkout without it
# serves the "not built" page from internal/webconfig/assets.go.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
cd "$repo_root"

pnpm install
pnpm --filter @statusloom/configurator build

dist_src="$repo_root/apps/configurator/dist"
dist_dst="$repo_root/internal/webconfig/dist"

mkdir -p "$dist_dst"
# Clear the previous build but keep .gitignore: it is the only tracked entry in
# dist/, and //go:embed all:dist needs the directory to exist at all.
find "$dist_dst" -mindepth 1 ! -name .gitignore -delete
cp -R "$dist_src"/. "$dist_dst"/

echo
echo "Built web assets copied into internal/webconfig/dist (git-ignored)."
