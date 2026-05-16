#!/bin/zsh
# install-service.sh — render the launchd plist from the template and load it.
#
# Idempotent: safe to re-run. Existing plist is overwritten and the service
# is rebootstrapped so it picks up any plist changes.
#
# Paths follow the convention used by ~/bin/aurelia:
#   plist   → ~/Library/LaunchAgents/com.aurelia.agent.plist
#   binary  → ~/.aurelia/bin/aurelia
#   logs    → ~/.aurelia/logs/

set -euo pipefail

LABEL="com.aurelia.agent"
PLIST_DEST="$HOME/Library/LaunchAgents/${LABEL}.plist"
BINARY="$HOME/.aurelia/bin/aurelia"
LOG_DIR="$HOME/.aurelia/logs"
DOMAIN="gui/$(id -u)"
SERVICE="${DOMAIN}/${LABEL}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEMPLATE="${SCRIPT_DIR}/${LABEL}.plist.tmpl"

if [[ ! -f "$TEMPLATE" ]]; then
    print -r -- "error: template not found: $TEMPLATE" >&2
    exit 1
fi

mkdir -p "$HOME/Library/LaunchAgents" "$LOG_DIR" "$(dirname "$BINARY")"

# Render template — substitute __HOME__, __BINARY__, __PATH__
# PATH is captured from the current shell so the daemon inherits the same
# tool resolution (npm/node/git/gh) that the user has interactively.
sed \
    -e "s|__HOME__|${HOME}|g" \
    -e "s|__BINARY__|${BINARY}|g" \
    -e "s|__PATH__|${PATH}|g" \
    "$TEMPLATE" > "${PLIST_DEST}.new"
mv "${PLIST_DEST}.new" "$PLIST_DEST"

print -r -- "installed: $PLIST_DEST"

# Rebootstrap so changes (and a missing service) get picked up.
if launchctl print "$SERVICE" >/dev/null 2>&1; then
    launchctl bootout "$SERVICE" || true
fi
launchctl bootstrap "$DOMAIN" "$PLIST_DEST"
launchctl kickstart -k "$SERVICE"

print -r -- "service loaded: $SERVICE"
print -r -- "follow logs:    tail -f $LOG_DIR/aurelia.stderr.log"
