#!/bin/bash
# Check that the trusted Deno adapter committed at <commit> is the one scripts/gen/deno_adapter.sh builds from that
# commit's sdk/ts/src (#6001). pre-push runs it when the pushed commits change the adapter or what it is built from;
# by hand: scripts/hooks/check-deno-adapter.sh HEAD. It builds from the commit, never the worktree, with the commit's own
# generator and the Bun version .github/workflows/server-test.yml pins (another version builds other bytes): from
# ~/.bun-<version>/node_modules/.bin or ~/.bun-<version>/bin when it is there, else bun on PATH. Without bun, or with
# another version whose build differs, it refuses and says how to install the pinned one. See
# docs/operations/testing.md, "Trusted Deno adapter".
set -u
commit="${1:?usage: check-deno-adapter.sh <commit>}"
adapter=server/internal/extension/denohost/adapter.mjs
git cat-file -e "$commit:server/internal/extension/denohost/adapter.ts" 2>/dev/null || exit 0 # no adapter to check
pinned="$(git show "$commit:.github/workflows/server-test.yml" 2>/dev/null |
  sed -n "s/^[[:space:]]*bun-version:[[:space:]]*['\"]\{0,1\}\([0-9][0-9.]*\).*/\1/p" | head -1)"
pinned="${pinned:-the one .github/workflows/server-test.yml pins}"
for dir in "$HOME/.bun-$pinned/node_modules/.bin" "$HOME/.bun-$pinned/bin"; do
  if [ -x "$dir/bun" ]; then
    PATH="$dir:$PATH"
    break
  fi
done
install_hint() {
  echo "  Install Bun $pinned (pinned in .github/workflows/server-test.yml) where this check finds it:" >&2
  echo "    npm install --prefix ~/.bun-$pinned bun@$pinned" >&2
  echo "  then, if the adapter is stale, regenerate: PATH=~/.bun-$pinned/node_modules/.bin:\$PATH scripts/gen/deno_adapter.sh" >&2
  echo "  and commit $adapter." >&2
}
if ! command -v bun >/dev/null 2>&1; then
  echo "pre-push: these commits change the trusted Deno adapter or its sources (sdk/ts/src), and there is no bun to check $adapter." >&2
  install_hint
  exit 1
fi
tmp="$(mktemp -d)" || exit 1
trap 'rm -rf "${tmp:?}"' EXIT
if ! git archive --format=tar "$commit" sdk/ts server/internal/extension/denohost scripts/gen/deno_adapter.sh | tar -x -C "$tmp"; then
  echo "pre-push: cannot read the trusted Deno adapter's sources from $commit" >&2
  exit 1
fi
cp "$tmp/$adapter" "$tmp/committed.mjs" || exit 1
if ! bash "$tmp/scripts/gen/deno_adapter.sh" >"$tmp/build.log" 2>&1; then
  echo "pre-push: building the trusted Deno adapter from $commit failed:" >&2
  sed 's/^/  /' "$tmp/build.log" >&2
  exit 1
fi
cmp -s "$tmp/committed.mjs" "$tmp/$adapter" && exit 0
version="$(bun --version 2>/dev/null)"
if [ "$version" != "$pinned" ]; then
  echo "pre-push: cannot check $adapter: bun is ${version:-of unknown version}, not the pinned $pinned, and builds other bytes." >&2
  install_hint
  exit 1
fi
echo "pre-push: $adapter is stale: it is not what $commit's sdk/ts/src builds with Bun $pinned." >&2
echo "  Regenerate: PATH=$(dirname "$(command -v bun)"):\$PATH scripts/gen/deno_adapter.sh, then commit $adapter." >&2
exit 1
