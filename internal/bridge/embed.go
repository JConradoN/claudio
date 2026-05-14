package bridge

import _ "embed"

// EmbeddedBundleJS contains the pre-built bridge bundle.
// When empty/nil, the bridge will be built from TypeScript source on first use.
var EmbeddedBundleJS []byte

// EmbeddedBridgeTS contains the TypeScript bridge source used to build
// bundle.js on first run when no pre-built bundle is embedded.
//
//go:embed bundle.ts
var EmbeddedBridgeTS []byte
