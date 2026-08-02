#!/usr/bin/env bash
# Regenerates the README's media (docs/media) by driving the real configurator
# in Chromium via scripts/capture-media.ts.
#
# The configurator is started against a THROWAWAY config directory, never the
# developer's own: the capture script edits the document it is shown, so it must
# not be pointed at a live configuration. STATUSLOOM_CONFIG / STATUSLOOM_CACHE_DIR
# and CLAUDE_CONFIG_DIR are all redirected into a temp dir that is removed on
# exit, and the account fields are fed a fixture profile so the captures do not
# leak the developer's email or organization.
#
# Requires the repo's dev dependencies plus a chromium and ffmpeg:
#   pnpm install
#   pnpm exec playwright install chromium
#   brew install ffmpeg   (or your platform's ffmpeg)
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
cd "$repo_root"

work="$(mktemp -d)"
server_pid=""

cleanup() {
    stop_server
    rm -rf "$work"
}
trap cleanup EXIT

stop_server() {
    [[ -z "$server_pid" ]] && return 0
    kill "$server_pid" 2>/dev/null || true
    for _ in $(seq 1 12); do
        kill -0 "$server_pid" 2>/dev/null || { server_pid=""; return 0; }
        sleep 0.25
    done
    kill -9 "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
    server_pid=""
}

# Starts a configurator on an EMPTY store and echoes its tokenized URL. Each
# capture phase gets its own: both phases edit the document they are shown, so a
# shared store would leak one phase's edits into the other.
start_server() {
    rm -f "$work/statusloom.json"
    : > "$work/server.log"
    env STATUSLOOM_CONFIG="$work/statusloom.json" \
        STATUSLOOM_CACHE_DIR="$work/cache" \
        CLAUDE_CONFIG_DIR="$work/claude" \
        STATUSLOOM_NO_USAGE_API=1 \
        "$work/statusloom" config -no-browser > "$work/server.log" 2>&1 &
    server_pid=$!

    local url=""
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
    echo "$url"
}

mkdir -p "$work/claude" "$work/cache"
# A fixture account, so account-* fields and variant conditions can be shown
# without capturing real identity.
cat > "$work/claude/.claude.json" <<'EOF'
{
  "oauthAccount": {
    "emailAddress": "you@example.com",
    "displayName": "Example User",
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

for phase in anim span stills; do
    echo "Capturing ($phase) ..."
    url="$(start_server)"
    env UI_URL="$url" MEDIA_OUT="$repo_root/docs/media" CAPTURE_PHASE="$phase" \
        node --experimental-strip-types "$script_dir/capture-media.ts"
    stop_server
done
