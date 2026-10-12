#!/usr/bin/env bash
# docs-redirects.sh <out-dir>: a page for every URL the old docs site served, sending it to the same page on mockz.io.
set -euo pipefail

OUT="${1:?usage: docs-redirects.sh <out-dir>}"
BASE="https://mockz.io/docs/mock-server/"

# Old path, then its path under BASE. The list is what mkdocs published, so a new docs page needs no entry.
PAGES=(
  ":"
  "how-it-works/:how-it-works/"
  "usage/portable/:usage/portable/"
  "usage/docker-compose/:usage/docker-compose/"
  "usage/codegen/:usage/codegen/"
  "templates/:templates/"
  "services/:services/"
  "contexts/:contexts/"
  "fake-list/:fake-list/"
  "factory/:factory/"
  "config/app/:config/app/"
  "config/service/:config/service/"
  "config/codegen/:config/codegen/"
  "commands/service/:commands/service/"
  "commands/generate/:commands/generate/"
  "commands/lint/:commands/lint/"
  "commands/simplify/:commands/simplify/"
  "replay/:replay/"
  "storage/:storage/"
  "middleware/:middleware/"
  "api/:"
)

page() {
  cat <<EOF
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Mockzilla docs moved</title>
<link rel="canonical" href="$1">
<meta http-equiv="refresh" content="0; url=$1">
<script>location.replace("$1" + location.hash)</script>
</head>
<body>
<p>The Mockzilla docs moved to <a href="$1">$1</a>.</p>
</body>
</html>
EOF
}

rm -rf "$OUT"
mkdir -p "$OUT"
touch "$OUT/.nojekyll"
for entry in "${PAGES[@]}"; do
  old="${entry%%:*}"
  mkdir -p "$OUT/$old"
  page "$BASE${entry#*:}" > "$OUT/${old}index.html"
done
page "$BASE" > "$OUT/404.html"
