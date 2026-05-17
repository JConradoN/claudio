#!/bin/bash
# install-systemd.sh — install Aurelia as a user systemd service.
#
# Idempotent: safe to re-run. Existing service file is overwritten.
#
# Paths follow the convention used by ~/.aurelia/bin/aurelia:
#   service → ~/.config/systemd/user/aurelia.service
#   binary  → ~/.aurelia/bin/aurelia

set -euo pipefail

SERVICE_NAME="aurelia"
SERVICE_FILE="$HOME/.config/systemd/user/${SERVICE_NAME}.service"
BINARY="$HOME/.aurelia/bin/aurelia"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEMPLATE="${SCRIPT_DIR}/aurelia.service.tmpl"

if [[ ! -f "$TEMPLATE" ]]; then
    echo "error: template not found: $TEMPLATE" >&2
    exit 1
fi

mkdir -p "$HOME/.config/systemd/user" "$(dirname "$BINARY")"

# Render template
sed \
    -e "s|__HOME__|${HOME}|g" \
    -e "s|__BINARY__|${BINARY}|g" \
    -e "s|__PATH__|${PATH}|g" \
    "$TEMPLATE" > "${SERVICE_FILE}.new"
mv "${SERVICE_FILE}.new" "$SERVICE_FILE"

echo "installed: $SERVICE_FILE"

# Reload systemd and enable/start service
systemctl --user daemon-reload
systemctl --user enable "$SERVICE_NAME"

if systemctl --user is-active "$SERVICE_NAME" >/dev/null 2>&1; then
    systemctl --user restart "$SERVICE_NAME"
    echo "service restarted: $SERVICE_NAME"
else
    systemctl --user start "$SERVICE_NAME"
    echo "service started: $SERVICE_NAME"
fi

echo ""
echo "View logs: journalctl --user -u aurelia -f"
echo "Stop:      systemctl --user stop aurelia"
echo "Disable:   systemctl --user disable aurelia"
