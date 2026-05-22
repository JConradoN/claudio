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
#
# CRITICAL: The certificate MUST use basicConstraints=CA:FALSE. Apple's codesign
# rejects CA certificates — security find-identity -p codesigning returns 0
# identities, and TCC does not honor the signature, causing permission prompts
# on every rebuild.

set -euo pipefail

CERT_NAME="Aurelia Dev"
KEYCHAIN_NAME="aurelia-codesign"
KEYCHAIN_PATH="$HOME/Library/Keychains/${KEYCHAIN_NAME}.keychain-db"
KEYCHAIN_PASS_FILE="$HOME/.aurelia/codesign-pass"

echo "==> Creating dedicated keychain: ${KEYCHAIN_NAME}"
mkdir -p "$(dirname "$KEYCHAIN_PASS_FILE")"

# Always recreate the keychain to ensure a clean slate.
# The old one may have a cert with CA:TRUE from a previous script version.
echo "    removing old keychain (clean slate)..."
security delete-keychain "$KEYCHAIN_PATH" 2>/dev/null || true
rm -f "$KEYCHAIN_PATH"

KEYCHAIN_PASS=$(openssl rand -base64 24)
security create-keychain -p "$KEYCHAIN_PASS" "$KEYCHAIN_PATH"
echo "$KEYCHAIN_PASS" > "$KEYCHAIN_PASS_FILE"
chmod 600 "$KEYCHAIN_PASS_FILE"
echo "    created (password stored in ${KEYCHAIN_PASS_FILE})"

# Add to search list so codesign can find it without --keychain
security list-keychains -d user -s \
    "$KEYCHAIN_PATH" \
    "$HOME/Library/Keychains/login.keychain-db"

# Unlock and keep unlocked for 24h (long enough for dev sessions)
security unlock-keychain -p "$KEYCHAIN_PASS" "$KEYCHAIN_PATH"
security set-keychain-settings -t 86400 "$KEYCHAIN_PATH"

echo "==> Generating self-signed code signing certificate: ${CERT_NAME}"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# Generate certificate with CA:FALSE (required for code signing on macOS).
# Also set extendedKeyUsage=codeSigning and keyUsage=digitalSignature.
openssl req -x509 -newkey rsa:2048 -days 3650 -nodes \
    -keyout "$TMPDIR/codesign.key" \
    -out "$TMPDIR/codesign.crt" \
    -subj "/CN=${CERT_NAME}/O=Aurelia" \
    -addext "basicConstraints=critical,CA:FALSE" \
    -addext "extendedKeyUsage=codeSigning" \
    -addext "keyUsage=digitalSignature" 2>/dev/null

# Create PKCS#12 bundle — required by macOS to properly associate key + cert.
# Using PKCS#12 with security import -f pkcs12 is the most reliable method.
P12_PASS="import-pass"
openssl pkcs12 -export \
    -inkey "$TMPDIR/codesign.key" \
    -in "$TMPDIR/codesign.crt" \
    -out "$TMPDIR/codesign.p12" \
    -passout pass:"$P12_PASS" \
    -name "$CERT_NAME" 2>/dev/null

echo "    importing PKCS#12..."
security import "$TMPDIR/codesign.p12" \
    -k "$KEYCHAIN_PATH" \
    -f pkcs12 \
    -P "$P12_PASS" \
    -A 2>/dev/null

# Set partition list to allow codesign (and other Apple tools) to access the key.
# Retry up to 3 times since the keychain may not be fully indexed after import.
for i in 1 2 3; do
    if security set-key-partition-list \
        -S apple-tool:,apple:,codesign: \
        -s \
        -k "$KEYCHAIN_PASS" \
        "$KEYCHAIN_PATH" 2>/dev/null; then
        break
    fi
    sleep 1
done

echo ""
echo "==> Verification"

# Note: On macOS Sequoia+, security find-identity -p codesigning may still show
# "0 valid identities" for self-signed certs due to stricter validation. This is
# a known macOS quirk — codesign works regardless. The test sign below validates.
echo "    --- security find-identity (may show 0 for self-signed on Sequoia+) ---"
IDENTITY=$(security find-identity -p codesigning -v "$KEYCHAIN_PATH" 2>/dev/null | grep "$CERT_NAME" | head -1)
if [[ -n "$IDENTITY" ]]; then
    echo "    ✅ Identity found: ${IDENTITY}"
fi

# Fallback: check that the cert exists with the right attributes
CERT_EXISTS=$(security find-certificate -c "$CERT_NAME" -p "$KEYCHAIN_PATH" 2>/dev/null)
if [[ -z "$CERT_EXISTS" ]]; then
    echo "    ❌ Certificate not found in keychain."
    exit 1
fi

# Verify basicConstraints is CA:FALSE
if echo "$CERT_EXISTS" | openssl x509 -noout -ext basicConstraints 2>/dev/null | grep -q "CA:FALSE"; then
    echo "    ✅ Certificate has basicConstraints=CA:FALSE"
else
    echo "    ❌ Certificate has CA:TRUE — will NOT work for code signing."
    echo "       Regenerate with -addext \"basicConstraints=critical,CA:FALSE\""
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
echo "If you still get permission prompts after the first deploy:"
echo "  System Settings → Privacy & Security → confirm any prompts"
echo ""
echo "Note: macOS Sequoia may still prompt once after cert rotation."
echo "Subsequent builds with the same certificate should NOT prompt."
echo "For a permanent fix without prompts, join Apple Developer (\$99/yr)"
echo "and use a Developer ID certificate."
