#!/usr/bin/env bash
# Runs scripts/ui-check.ts against a throwaway configurator: the browser-level
# checks that the vitest suite cannot express (measured geometry, soft wrapping,
# layout stability).
#
# The configurator is started against a DISPOSABLE config directory, never the
# developer's own — the check edits the document it is shown. STATUSLOOM_CONFIG /
# STATUSLOOM_CACHE_DIR and CLAUDE_CONFIG_DIR all point into a temp dir that is
# removed on exit, and the account fields are fed a fixture profile so results do
# not depend on which account happens to be logged in (a real claude_team account
# made a variant `when` gate evaluate true and broke a check).
#
# The store is empty at start: state left over from a previous run has already
# produced a false negative once.
#
# Requires the repo's dev dependencies plus a chromium:
#   pnpm install
#   pnpm exec playwright install chromium
#
# Screenshots and results.json land in the directory given by UI_OUT
# (default: a temp dir, printed at the end).
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
cd "$repo_root"

work="$(mktemp -d)"
out="${UI_OUT:-$work/shots}"
server_pid=""

cleanup() {
    if [[ -n "$server_pid" ]] && kill -0 "$server_pid" 2>/dev/null; then
        kill "$server_pid" 2>/dev/null || true
        sleep 0.5
        kill -9 "$server_pid" 2>/dev/null || true
        wait "$server_pid" 2>/dev/null || true
    fi
    # Keep the screenshots when they were written outside the temp dir.
    if [[ "$out" == "$work"/* ]]; then
        echo "screenshots were in $out (removed with the temp dir; set UI_OUT to keep them)"
    fi
    rm -rf "$work"
}
trap cleanup EXIT

mkdir -p "$work/claude" "$work/cache" "$out"
cat > "$work/claude/.claude.json" <<'EOF'
{
  "oauthAccount": {
    "emailAddress": "ui-check@example.com",
    "displayName": "UI Check",
    "organizationName": "Example Org",
    "organizationRole": "primary_owner",
    "organizationType": "claude_team",
    "seatTier": "team_tier_1",
    "userRateLimitTier": "default_claude_team_1x"
  },
  "projects": {}
}
EOF

echo "Building the frontend and the binary ..."
"$script_dir/build-web.sh" > /dev/null
go build -o "$work/statusloom" ./cmd/statusloom

echo "Starting an isolated configurator on a free port ..."
env STATUSLOOM_CONFIG="$work/statusloom.json" \
    STATUSLOOM_CACHE_DIR="$work/cache" \
    CLAUDE_CONFIG_DIR="$work/claude" \
    STATUSLOOM_NO_USAGE_API=1 \
    "$work/statusloom" config -no-browser > "$work/server.log" 2>&1 &
server_pid=$!

url=""
for _ in $(seq 1 20); do
    sleep 0.5
    url="$(grep -o "http://127.0.0.1:[0-9]*[^ ]*" "$work/server.log" | head -1 || true)"
    [[ -n "$url" ]] && break
done
if [[ -z "$url" ]]; then
    echo "the configurator did not start; log follows" >&2
    cat "$work/server.log" >&2
    exit 1
fi

env UI_URL="$url" UI_OUT="$out" node --experimental-strip-types "$script_dir/ui-check.ts"
