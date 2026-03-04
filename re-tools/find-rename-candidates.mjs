#!/usr/bin/env node
// Find high-value rename candidates in section files.
// Shows short identifiers with their definitions and usage counts.
// Usage: node tools/find-rename-candidates.mjs [--min-count 50] [--max-len 3]

import { readFileSync, readdirSync } from "fs";
import { join } from "path";

const SECTIONS_DIR = "src/sections";
const SAFE_BOUNDARY = /[a-zA-Z0-9_$\u0080-\uFFFF]/;

const args = process.argv.slice(2);
const minCount = parseInt(args[args.indexOf("--min-count") + 1] || "50");
const maxLen = parseInt(args[args.indexOf("--max-len") + 1] || "3");

// JS keywords and common names to skip
const SKIP = new Set([
  "if", "in", "do", "for", "new", "var", "let", "try", "get", "set",
  "map", "has", "add", "all", "any", "key", "now", "pop", "min", "max",
  "log", "end", "run", "put", "raw", "err", "msg", "url", "dir", "env",
  "cwd", "pid", "uid", "gid", "tty", "arg", "buf", "len", "val", "num",
  "str", "obj", "arr", "req", "res", "ref", "ret", "src", "dst", "tmp",
  "sum", "cnt", "pos", "col", "row", "tag", "doc", "dom", "css", "svg",
  "img", "alt", "api", "cli", "npm", "git", "cmd", "Map", "Set", "URL",
  "on", "of", "to", "is", "or", "no", "ok", "fs",
  // Already renamed
  "BASH_TOOL_NAME", "READ_TOOL_NAME", "EDIT_TOOL_NAME", "WRITE_TOOL_NAME",
  "GLOB_TOOL_NAME", "GREP_TOOL_NAME", "WEB_FETCH_TOOL_NAME",
  "WEB_SEARCH_TOOL_NAME", "NOTEBOOK_EDIT_TOOL_NAME", "TODO_WRITE_TOOL_NAME",
  "cliMain", "setupCommander", "showSetupScreens", "completeOnboarding",
  "loadManagedSettings", "isDebuggerAttached", "initHeadless", "headlessExports",
  "initApp", "appExports", "webFetchTool", "webFetchInputSchema",
  "webFetchOutputSchema", "checkToolPermissions", "agentLoop",
  "countMessageTokens", "readTool",
]);

const files = readdirSync(SECTIONS_DIR)
  .filter(f => f.endsWith(".js"))
  .sort()
  .map(f => join(SECTIONS_DIR, f));

// Count identifier occurrences
const counts = {};
const fileOccurrences = {};

for (const file of files) {
  const src = readFileSync(file, "utf-8");
  const re = /\b([a-zA-Z_$][a-zA-Z0-9_$]*)\b/g;
  let m;
  while ((m = re.exec(src)) !== null) {
    const id = m[1];
    if (id.length > maxLen || SKIP.has(id)) continue;
    counts[id] = (counts[id] || 0) + 1;
    if (!fileOccurrences[id]) fileOccurrences[id] = {};
    const basename = file.split("/").pop();
    fileOccurrences[id][basename] = (fileOccurrences[id][basename] || 0) + 1;
  }
}

// Find definitions for each candidate
function findDefinition(id) {
  const defs = [];
  const escaped = id.replace(/\$/g, "\\$");

  for (const file of files) {
    const src = readFileSync(file, "utf-8");
    const lines = src.split("\n");
    const patterns = [
      new RegExp(`^\\s*(?:var|let|const)\\s+${escaped}\\s*=`),
      new RegExp(`^\\s*function\\s+${escaped}\\s*\\(`),
    ];

    for (let i = 0; i < lines.length; i++) {
      for (const p of patterns) {
        if (p.test(lines[i])) {
          const context = lines.slice(i, i + 3).map(l => l.trim()).join(" | ");
          defs.push({
            file: file.split("/").pop(),
            line: i + 1,
            context: context.substring(0, 120),
          });
          if (defs.length >= 3) return defs;
        }
      }
    }
  }
  return defs;
}

// Sort and display
const candidates = Object.entries(counts)
  .filter(([, c]) => c >= minCount)
  .sort((a, b) => b[1] - a[1]);

console.log(`Found ${candidates.length} candidates (>=${minCount} occurrences, <=${maxLen} chars)\n`);

for (const [id, count] of candidates.slice(0, 60)) {
  const defs = findDefinition(id);
  const fileList = Object.entries(fileOccurrences[id])
    .sort((a, b) => b[1] - a[1])
    .map(([f, c]) => `${f.replace("src/sections/", "")}:${c}`)
    .join(", ");

  const defCount = defs.length;
  const safeToRename = defCount === 1 ? "✓" : defCount > 1 ? `✗ (${defCount} defs)` : "? (no def found)";

  console.log(`${id.padEnd(6)} ${String(count).padStart(5)} uses  ${safeToRename.padEnd(14)}  ${fileList}`);

  for (const d of defs.slice(0, 1)) {
    console.log(`  └─ ${d.file}:${d.line}: ${d.context}`);
  }
}
