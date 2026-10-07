#!/usr/bin/env node
// Parses every mermaid fence in the given Markdown files or directories; exits 1 on any failure.
// Usage: node validate-mermaid.mjs [file-or-dir ...]; defaults to ../../docs.

import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { JSDOM } from "jsdom";

// Mermaid reads the DOM while parsing, so a minimal jsdom window stands in.
const dom = new JSDOM("<!DOCTYPE html><body></body>", { pretendToBeVisual: true });
globalThis.window = dom.window;
globalThis.document = dom.window.document;

const mermaid = (await import("mermaid")).default;
mermaid.initialize({ startOnLoad: false });

function collectMarkdown(path) {
  const st = statSync(path);
  if (st.isFile()) return path.endsWith(".md") ? [path] : [];
  return readdirSync(path).flatMap((entry) => collectMarkdown(join(path, entry)));
}

function extractMermaidBlocks(text) {
  const lines = text.split("\n");
  const blocks = [];
  let current = null;
  lines.forEach((line, i) => {
    if (current === null) {
      if (line.trim() === "```mermaid") current = { startLine: i + 1, body: [] };
    } else if (line.trim() === "```") {
      blocks.push({ code: current.body.join("\n"), startLine: current.startLine });
      current = null;
    } else {
      current.body.push(line);
    }
  });
  return blocks;
}

const inputs = process.argv.slice(2);
const roots = inputs.length > 0 ? inputs : ["../../docs"];

const files = roots.flatMap(collectMarkdown);
let total = 0;
let failed = 0;

for (const file of files) {
  const blocks = extractMermaidBlocks(readFileSync(file, "utf8"));
  for (const block of blocks) {
    total += 1;
    try {
      await mermaid.parse(block.code);
    } catch (err) {
      failed += 1;
      const msg = String(err?.message ?? err).split("\n")[0];
      console.error(`FAIL ${file}:${block.startLine}  ${msg}`);
    }
  }
}

const mermaidVersion = JSON.parse(
  readFileSync(new URL("./node_modules/mermaid/package.json", import.meta.url), "utf8"),
).version;

console.log(
  `mermaid-validate: ${total - failed}/${total} blocks valid (mermaid ${mermaidVersion})`,
);

if (failed > 0) {
  console.error(`mermaid-validate: ${failed} block(s) failed to parse`);
  process.exit(1);
}
