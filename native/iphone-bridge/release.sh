#!/usr/bin/env bash
# Release builds of persona-iphone-bridge for the platforms a Persona node runs on, for scripts/release/publish.sh
# (docs/extensions/iphone-bridge.md "Releases"). Builds only: build.sh runs the tests and lints.
#
#   native/iphone-bridge/release.sh <out dir> [<os>/<arch> …]
#
# Writes <out dir>/persona-iphone-bridge-<os>-<arch> for linux/amd64, linux/arm64, darwin/arm64 and darwin/amd64 (or
# the platforms named). macOS builds need the Apple targets (rustup target add aarch64-apple-darwin
# x86_64-apple-darwin) on a Mac; Linux builds need zig and cargo-zigbuild (cargo install cargo-zigbuild), and are
# built against glibc 2.28. A platform this computer can't build is skipped with a "note:" line; the last line is
# "BRIDGE-RELEASE: <n> built, <m> skipped". Exits 1 only when a build that could run failed.
#
# Paths of this computer (the home directory, the checkout) are cut from the binaries' panic locations
# (--remap-path-prefix), so they carry nothing private (scripts/release/check_bundle_privacy.py).
set -uo pipefail

main() {
  local out="${1:?usage: release.sh <out dir> [<os>/<arch> …]}"
  shift
  local dir
  dir="$(cd "$(dirname "$0")" && pwd)"
  mkdir -p "$out" || return 1
  out="$(cd "$out" && pwd)"
  PATH="$HOME/.cargo/bin:$PATH"
  if ! command -v cargo >/dev/null 2>&1; then
    echo "note: no Rust toolchain here: no persona-iphone-bridge builds (install rustup)"
    echo "BRIDGE-RELEASE: 0 built, 4 skipped"
    return 0
  fi
  local platforms=("$@")
  [ ${#platforms[@]} -gt 0 ] || platforms=(linux/amd64 linux/arm64 darwin/arm64 darwin/amd64)
  local repo built=0 skipped=0 failed=0 p
  repo="$(cd "$dir/../.." && pwd)"
  export RUSTFLAGS="${RUSTFLAGS:-} --remap-path-prefix=$repo=. --remap-path-prefix=$HOME=~"
  export CARGO_TERM_QUIET=true
  for p in "${platforms[@]}"; do
    build_one "$dir" "$out" "$p"
    case $? in
      0) built=$((built + 1)) ;;
      2) skipped=$((skipped + 1)) ;;
      *) failed=$((failed + 1)) ;;
    esac
  done
  local summary="BRIDGE-RELEASE: $built built, $skipped skipped"
  [ "$failed" -gt 0 ] && summary+=", $failed failed"
  echo "$summary"
  [ "$failed" -eq 0 ]
}

# build_one <crate dir> <out dir> <os>/<arch>: 0 built, 2 skipped (this computer can't), 1 failed.
build_one() {
  local dir="$1" out="$2" os="${3%/*}" arch="${3#*/}" target cmd=(cargo build) rust_arch
  case "$arch" in
    amd64) rust_arch=x86_64 ;;
    arm64) rust_arch=aarch64 ;;
    *) echo "note: persona-iphone-bridge: unknown architecture $arch"; return 2 ;;
  esac
  case "$os" in
    darwin)
      target="$rust_arch-apple-darwin"
      [ "$(uname -s)" = Darwin ] || { echo "note: persona-iphone-bridge $os/$arch: macOS builds need a Mac"; return 2; }
      ;;
    linux)
      target="$rust_arch-unknown-linux-gnu"
      if ! command -v zig >/dev/null 2>&1 || ! command -v cargo-zigbuild >/dev/null 2>&1; then
        echo "note: persona-iphone-bridge $os/$arch: needs zig and cargo-zigbuild"
        return 2
      fi
      cmd=(cargo zigbuild)
      ;;
    *) echo "note: persona-iphone-bridge: no builds for $os"; return 2 ;;
  esac
  if command -v rustup >/dev/null 2>&1 && ! rustup target list --installed 2>/dev/null | grep -qx "$target"; then
    echo "note: persona-iphone-bridge $os/$arch: the Rust target $target isn't installed (rustup target add $target)"
    return 2
  fi
  local build_target="$target"
  [ "$os" = linux ] && build_target="$target.2.28"
  # The build's output stays out of <out dir>: the release scans everything there for private values, and a
  # compiler's output names this computer's paths.
  local log="${TMPDIR:-/tmp}/persona-iphone-bridge-$os-$arch.log"
  if ! (cd "$dir" && "${cmd[@]}" --locked --release --target "$build_target" >"$log" 2>&1); then
    echo "persona-iphone-bridge $os/$arch: FAILED (see $log)"
    return 1
  fi
  rm -f "$log"
  cp "${CARGO_TARGET_DIR:-$dir/target}/$target/release/persona-iphone-bridge" "$out/persona-iphone-bridge-$os-$arch" || return 1
  chmod 755 "$out/persona-iphone-bridge-$os-$arch"
  echo "persona-iphone-bridge $os/$arch: built"
  return 0
}

main "$@"
