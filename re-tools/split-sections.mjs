#!/usr/bin/env node
// Split cli.js into section files along natural module boundaries.
// Usage: node tools/split-sections.mjs
//
// This is a one-time mechanical split — no logic changes, just cutting
// at line boundaries. The build script reassembles them.

import { readFileSync, writeFileSync, mkdirSync } from "fs";

const INPUT = "src/cli.js";
const OUT_DIR = "src/sections";

// Split points: [startLine (1-indexed), filename, description]
// Each section starts at startLine and ends at the next section's startLine - 1.
const SECTIONS = [
  [1, "00-runtime.js", "Shebang, copyright, esbuild helpers (E/C/s1)"],
  [118, "01-zod-state.js", "Zod validation, i18n locales, session state, MCP transport"],
  [15099, "02-network.js", "ws, AJV, Axios, HTTP utilities"],
  [53849, "03-providers.js", "OpenTelemetry, AWS SDK, gRPC, Azure, provider router"],
  [125449, "04-react-ink.js", "React 18, Ink 3, yoga-layout, Sharp, permission modes"],
  [180442, "05-app-core.js", "Tools, permissions, MCP, API client, auth"],
  [416115, "06-app-ui.js", "Terminal backends, CLI commands, UI components, dialogs"],
  [527662, "07-app-features.js", "Tree-sitter, screenshot, voice, REPL, teleport"],
  [597952, "08-entry.js", "Main bootstrap, CLI setup, entry point"],
];

const lines = readFileSync(INPUT, "utf-8").split("\n");
console.log(`Read ${lines.length} lines from ${INPUT}`);

mkdirSync(OUT_DIR, { recursive: true });

for (let i = 0; i < SECTIONS.length; i++) {
  const [start, filename, desc] = SECTIONS[i];
  const end = i + 1 < SECTIONS.length ? SECTIONS[i + 1][0] - 1 : lines.length;
  const startIdx = start - 1; // convert to 0-indexed
  const endIdx = end; // slice is exclusive

  const sectionLines = lines.slice(startIdx, endIdx);
  const outPath = `${OUT_DIR}/${filename}`;
  const isLast = i === SECTIONS.length - 1;
  // Each section ends with \n so concatenation preserves line breaks.
  // Last section keeps original ending (no extra newline).
  writeFileSync(outPath, sectionLines.join("\n") + (isLast ? "" : "\n"));

  console.log(
    `  ${filename}: lines ${start}-${end} (${sectionLines.length} lines) — ${desc}`
  );
}

console.log(`\nDone. ${SECTIONS.length} section files written to ${OUT_DIR}/`);
