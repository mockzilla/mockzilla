#!/usr/bin/env bash
# Copies the third-party UI assets out of their npm packages into resources/ui/lib.
#
#   ui-assets.sh                     fetch the pinned versions
#   ui-assets.sh bump                pin every package to its latest version, then fetch
#   ui-assets.sh bump <pkg> [<ver>]  pin one package to <ver> (default latest), then fetch
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PINS="$ROOT/scripts/ui-assets.versions"
LIB="$ROOT/resources/ui/lib"
REGISTRY="https://registry.npmjs.org"

ACE_MODES="json yaml xml html text markdown"
ACE_WORKERS="base json yaml xml html"
FA_FONTS="fa-solid-900 fa-regular-400"

latest() {
    curl -fsSL "$REGISTRY/$1/latest" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p'
}

pin() {
    local pkg="$1" version="$2"
    awk -v pkg="$pkg" -v version="$version" '$1 == pkg { $2 = version } { print }' "$PINS" > "$PINS.tmp"
    mv "$PINS.tmp" "$PINS"
    echo "pinned $pkg $version"
}

unpack() {
    local pkg="$1" version="$2" dest="$3"
    mkdir -p "$dest"
    curl -fsSL "$REGISTRY/$pkg/-/${pkg##*/}-$version.tgz" | tar -xz -C "$dest" --strip-components=1
}

copy() {
    local pkg="$1" src="$2"
    case "$pkg" in
        ace-builds)
            mkdir -p "$LIB/ace"
            cp "$src/src-min-noconflict/ace.js" "$src/src-min-noconflict/ext-searchbox.js" "$LIB/ace/"
            cp "$src"/src-min-noconflict/theme-*.js "$LIB/ace/"
            for mode in $ACE_MODES; do cp "$src/src-min-noconflict/mode-$mode.js" "$LIB/ace/"; done
            for worker in $ACE_WORKERS; do cp "$src/src-min-noconflict/worker-$worker.js" "$LIB/ace/"; done
            ;;
        @fortawesome/fontawesome-free)
            mkdir -p "$LIB/fontawesome/css" "$LIB/fontawesome/webfonts"
            cp "$src/css/all.min.css" "$LIB/fontawesome/css/"
            for font in $FA_FONTS; do cp "$src/webfonts/$font.woff2" "$LIB/fontawesome/webfonts/"; done
            ;;
        normalize.css)
            mkdir -p "$LIB/normalize"
            cp "$src/normalize.css" "$LIB/normalize/"
            ;;
        js-yaml)
            mkdir -p "$LIB/js-yaml"
            cp "$src/dist/js-yaml.min.js" "$LIB/js-yaml/"
            ;;
        *)
            echo "no copy rule for $pkg" >&2
            exit 1
            ;;
    esac
}

if [ "${1:-}" = "bump" ]; then
    if [ -n "${2:-}" ]; then
        grep -q "^$2 " "$PINS" || { echo "$2 is not in $PINS" >&2; exit 1; }
        pin "$2" "${3:-$(latest "$2")}"
    else
        while read -r pkg _; do pin "$pkg" "$(latest "$pkg")"; done < <(cat "$PINS")
    fi
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

rm -rf "$LIB"
while read -r pkg version; do
    echo "fetching $pkg $version"
    unpack "$pkg" "$version" "$TMP/$pkg"
    copy "$pkg" "$TMP/$pkg"
done < "$PINS"
