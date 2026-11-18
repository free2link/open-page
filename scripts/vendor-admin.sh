#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
archive=$(npm pack @awesome.me/webawesome@3.15.0 --pack-destination "$work" --silent)
tar -xzf "$work/$archive" -C "$work"
mkdir -p admin/vendor
for directory in styles chunks components/badge; do
  mkdir -p "admin/vendor/$directory"
  cp -R "$work/package/dist-cdn/$directory/." "admin/vendor/$directory/"
done
find admin/vendor -type f \( -name '*.ts' -o -name '*.map' \) -delete
cp "$work/package/LICENSE.md" admin/vendor/LICENSE.md
