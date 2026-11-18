#!/usr/bin/env bash
# Regenerate the trusted source-runtime adapter; no extension lifecycle scripts run.
# Minified for syntax and whitespace, not identifiers: Bun's identifier minifier renames symbols whenever the SDK's set
# of symbols changes, so a type-only edit to sdk/ts/src changed the adapter's bytes and staled it (#6001).
# TestTrustedAdapterMatchesOfficialSDK builds with the same flags; scripts/hooks/check-deno-adapter.sh runs this script.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
bun build "$ROOT/server/internal/extension/denohost/adapter.ts" --target=browser --minify-syntax --minify-whitespace --outfile "$ROOT/server/internal/extension/denohost/adapter.mjs"
