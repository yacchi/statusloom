#!/usr/bin/env bash
# Restores internal/webconfig/dist to exactly one file: the placeholder
# index.html tracked in git. Run this before committing after
# scripts/build-web.sh has replaced the directory with a real Vite build.
#
# The placeholder content below is embedded directly (not copied from the
# working tree) so this script is correct even if the placeholder file has
# already been overwritten by a build.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
dist_dir="$repo_root/internal/webconfig/dist"

rm -rf "$dist_dir"
mkdir -p "$dist_dir"

cat > "$dist_dir/index.html" <<'EOF'
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Statusloom configurator</title>
</head>
<body>
<p>Statusloom configurator UI has not been built yet. Run scripts/build-web.sh and rebuild.</p>
</body>
</html>
EOF

echo "internal/webconfig/dist restored to the placeholder index.html."
