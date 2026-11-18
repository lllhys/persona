#!/usr/bin/env python3
"""Check the packaged Android variants' DEBUG-only sort and answer JNI boundaries, without a device."""
from __future__ import annotations

import argparse
import subprocess
from pathlib import Path

SHADOW_SYMBOLS = {
    "Java_persona_core_rustcore_NativeBridge_projectSort": "sort shadow",
    "Java_persona_core_rustcore_NativeBridge_projectAnswer": "answer shadow",
    "Java_persona_core_rustcore_NativeBridge_displayAnswer": "answer shadow",
}


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("root", type=Path, help="Directory holding <variant>/<abi>/libpersona_core.so")
    parser.add_argument("readelf", type=Path)
    args = parser.parse_args()
    found = False
    libraries = sorted(args.root.glob("*/libpersona_core.so"))
    if not libraries:
        raise SystemExit(f"Release native guard found no libraries under {args.root}")
    for library in libraries:
        output = subprocess.check_output([str(args.readelf), "--dyn-syms", str(library)], text=True)
        exported = sorted(symbol for symbol in SHADOW_SYMBOLS if symbol in output)
        print(f"{library}: DEBUG-only exports={exported or 'none'}")
        found |= bool(exported)
    if found:
        raise SystemExit("Release Android native libraries must not export DEBUG-only sort or answer shadows")
    print("PASS: Release Android native libraries have no sort or answer shadow JNI exports")


if __name__ == "__main__":
    main()
