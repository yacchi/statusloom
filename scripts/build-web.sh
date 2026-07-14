#!/usr/bin/env bash
# Builds the configurator frontend (apps/configurator) and copies the
# resulting Vite build over internal/webconfig/dist, replacing the
# placeholder index.html so `go build`/`go run` embed the real UI.
#
# IMPORTANT: the built assets this script produces must NOT be committed.
# Run scripts/clean-web.sh to restore the placeholder before committing.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
cd "$repo_root"

pnpm install
pnpm --filter @statusloom/configurator build

dist_src="$repo_root/apps/configurator/dist"
dist_dst="$repo_root/internal/webconfig/dist"

rm -rf "$dist_dst"
mkdir -p "$dist_dst"
cp -R "$dist_src"/. "$dist_dst"/

echo
echo "Built web assets copied into internal/webconfig/dist."
echo "Reminder: these built assets are NOT to be committed."
echo "Run scripts/clean-web.sh to restore the placeholder before committing."
