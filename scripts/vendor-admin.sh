#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
repo=$PWD
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

mkdir -p "$work/editor"
cd "$work/editor"
npm init -y >/dev/null
npm install --silent --no-audit --no-fund \
  codemirror@6 @codemirror/lang-html@6 @codemirror/lang-css@6 \
  @codemirror/lang-javascript@6 @codemirror/view@6 @codemirror/commands@6 esbuild@0.25
cat > entry.js <<'EOF'
import { basicSetup, EditorView } from "codemirror";
import { keymap } from "@codemirror/view";
import { indentWithTab } from "@codemirror/commands";
import { html } from "@codemirror/lang-html";
import { css } from "@codemirror/lang-css";
import { javascript } from "@codemirror/lang-javascript";

function language(path) {
  if (path.endsWith(".css")) return css();
  if (/\.(js|mjs|cjs)$/.test(path)) return javascript();
  return html();
}

window.OpenPageEditor = {
  create(parent, { doc, path, readOnly, onSave }) {
    return new EditorView({
      parent,
      doc,
      extensions: [
        basicSetup,
        language(path),
        keymap.of([
          indentWithTab,
          { key: "Mod-s", preventDefault: true, run: () => { if (onSave) onSave(); return true; } },
        ]),
        EditorView.editable.of(!readOnly),
      ],
    });
  },
};
EOF
./node_modules/.bin/esbuild entry.js --bundle --minify --format=iife --legal-comments=inline \
  --outfile="$repo/admin/vendor/editor.js"
