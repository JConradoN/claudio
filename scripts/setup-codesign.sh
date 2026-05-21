#!/bin/zsh
# setup-codesign.sh — one-time setup: create self-signed code signing certificate.
#
# This certificate gives the Aurelia binary a stable code signing identity,
# so macOS TCC permissions (Accessibility, Files, Network, etc.) persist
# across rebuilds. Without it, each `make deploy` creates a "new" binary
# from macOS's perspective, re-prompting for permissions.
#
# Requires: openssl (system or brew), security
# Run once after initial install or when rotating the cert.

set -euo pipefail

CERT_NAME="Aurelia Dev"
KEYCHAIN_NAME="aurelia-codesign"
KEYCHAIN_PATH="$HOME/Library/Keychains/${KEYCHAIN_NAME}.keychain-db"
KEYCHAIN_PASS_FILE="$HOME/.aurelia/codesign-pass"
KEYCHAIN_PASS=$(openssl rand -base64 24)

echo "==> Creating dedicated keychain: ${KEYCHAIN_NAME}"

mkdir -p "$(dirname "$KEYCHAIN_PASS_FILE")"

# Remove existing keychain if present (clean slate)
if security find-identity -p codesigning "$KEYCHAIN_PATH" &>/dev/null 2>&1; then
    echo "    keychain already exists, skipping creation"
else
    security create-keychain -p "$KEYCHAIN_PASS" "$KEYCHAIN_PATH"
    echo "$KEYCHAIN_PASS" > "$KEYCHAIN_PASS_FILE"
    chmod 600 "$KEYCHAIN_PASS_FILE"
    echo "    created (password stored in ${KEYCHAIN_PASS_FILE})"
fi

# Add to search list so codesign can find it without --keychain
security list-keychains -d user -s \
    "$KEYCHAIN_PATH" \
    "$HOME/Library/Keychains/login.keychain-db"

# Unlock and keep unlocked for 24h (long enough for dev sessions)
security unlock-keychain -p "$KEYCHAIN_PASS" "$KEYCHAIN_PATH"
security set-keychain-settings -t 86400 "$KEYCHAIN_PATH"

# Allow codesign (and other Apple tools) to access the key
security set-key-partition-list \
    -S apple-tool:,apple:,codesign: \
    -s \
    -k "$KEYCHAIN_PASS" \
    "$KEYCHAIN_PATH" &>/dev/null

echo "==> Generating self-signed code signing certificate: ${CERT_NAME}"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

openssl req -x509 -newkey rsa:2048 -days 3650 -nodes \
    -keyout "$TMPDIR/codesign.key" \
    -out "$TMPDIR/codesign.crt" \
    -subj "/CN=${CERT_NAME}/O=Aurelia" \
    -addext "extendedKeyUsage=codeSigning" \
    -addext "keyUsage=digitalSignature" 2>/dev/null

# Import key first, then certificate
security import "$TMPDIR/codesign.key" \
    -k "$KEYCHAIN_PATH" \
    -P "$KEYCHAIN_PASS" \
    -A 2>/dev/null

security import "$TMPDIR/codesign.crt" \
    -k "$KEYCHAIN_PATH" \
    -P "$KEYCHAIN_PASS" \
    -A 2>/dev/null

echo ""
echo "==> Verification"

IDENTITY=$(security find-identity -p codesigning -v "$KEYCHAIN_PATH" 2>/dev/null | grep "$CERT_NAME" | head -1)
if [[ -n "$IDENTITY" ]]; then
    echo "    ✅ Certificate installed: ${IDENTITY}"
else
    echo "    ❌ Certificate not found — something went wrong"
    exit 1
fi

echo ""
echo "==> Testing: signing a test binary"

TEST_BIN="$TMPDIR/aurelia-test-sign"
cp /usr/bin/true "$TEST_BIN"
if codesign --force --sign "$CERT_NAME" -i "com.aurelia.agent" --options runtime "$TEST_BIN" 2>/dev/null; then
    REQ=$(codesign -d -r- "$TEST_BIN" 2>&1 | grep "designated")
    echo "    ✅ Signing works"
    echo "    Designated requirement: ${REQ}"
else
    echo "    ❌ Signing failed — check keychain permissions"
    exit 1
fi

echo ""
echo "==> Setup complete"
echo ""
echo "Next steps:"
echo "  make deploy   # rebuilds and signs the binary automatically"
echo ""
echo "If you still get permission prompts after deploy, run once interactively:"
echo "  codesign --force --sign '${CERT_NAME}' -i com.aurelia.agent --options runtime ~/.aurelia/bin/aurelia"
echo "  launchctl kickstart -k com.aurelia.agent"
