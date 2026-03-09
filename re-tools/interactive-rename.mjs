#!/usr/bin/env node
// Interactive rename tool for Klaudia codebase.
// Scans for short identifiers with a single definition, presents them
// with context, and lets you rename interactively with build+test safety.
//
// Usage:
//   node re-tools/interactive-rename.mjs                  # interactive mode
//   node re-tools/interactive-rename.mjs --scan           # scan only, no prompts
//   node re-tools/interactive-rename.mjs --min-len 2      # min identifier length (default: 2)
//   node re-tools/interactive-rename.mjs --max-len 5      # max identifier length (default: 5)
//   node re-tools/interactive-rename.mjs --min-count 10   # min occurrences (default: 10)
//   node re-tools/interactive-rename.mjs --file 05-app-core.js  # only scan one file
//   node re-tools/interactive-rename.mjs --dry-run        # don't write changes
//   node re-tools/interactive-rename.mjs --skip-test      # skip build+test (faster iteration)
//   node re-tools/interactive-rename.mjs --after XY7      # skip to after identifier XY7

import { readFileSync, writeFileSync, readdirSync, copyFileSync, mkdirSync, rmSync, existsSync } from "fs";
import { join } from "path";
import { execSync } from "child_process";
import { createInterface } from "readline";

const SECTIONS_DIR = "src/sections";
const SAFE_BOUNDARY = /[a-zA-Z0-9_$\u0080-\uFFFF]/;

// ─── Parse args ─────────────────────────────────────────────────────────────

const args = process.argv.slice(2);
function argVal(flag, def) {
  const i = args.indexOf(flag);
  return i !== -1 && args[i + 1] ? args[i + 1] : def;
}
const hasFlag = (f) => args.includes(f);

const MIN_LEN = parseInt(argVal("--min-len", "2"));
const MAX_LEN = parseInt(argVal("--max-len", "5"));
const MIN_COUNT = parseInt(argVal("--min-count", "10"));
const ONLY_FILE = argVal("--file", null);
const SCAN_ONLY = hasFlag("--scan");
const DRY_RUN = hasFlag("--dry-run");
const SKIP_TEST = hasFlag("--skip-test");
const AFTER_ID = argVal("--after", null);
const RENAME_LOG = "re-tools/rename-log.jsonl";

// ─── Skip list ──────────────────────────────────────────────────────────────

const JS_KEYWORDS = new Set([
  "if", "in", "do", "for", "new", "var", "let", "try", "get", "set",
  "else", "case", "this", "void", "with", "enum", "null", "true",
  "break", "catch", "class", "const", "super", "throw", "while",
  "yield", "false", "async", "await", "from", "export", "import",
  "return", "switch", "typeof", "delete", "static", "public",
  "default", "extends", "finally", "continue", "debugger", "function",
  "arguments", "instanceof", "of",
]);

const COMMON_SHORT = new Set([
  // Common method/property names that appear as standalone identifiers
  "map", "has", "add", "all", "any", "key", "now", "pop", "min", "max",
  "log", "end", "run", "put", "raw", "err", "msg", "url", "dir", "env",
  "cwd", "pid", "uid", "gid", "tty", "arg", "buf", "len", "val", "num",
  "str", "obj", "arr", "req", "res", "ref", "ret", "src", "dst", "tmp",
  "sum", "cnt", "pos", "col", "row", "tag", "doc", "dom", "css", "svg",
  "img", "alt", "api", "cli", "npm", "git", "cmd", "Map", "Set", "URL",
  "on", "of", "to", "is", "or", "no", "ok", "fs", "fn",
  "push", "pull", "path", "data", "size", "keys", "test", "type", "text",
  "name", "code", "node", "self", "call", "bind", "exec", "join", "trim",
  "sort", "find", "fill", "flat", "from", "each", "some", "once", "next",
  "then", "done", "open", "read", "pipe", "emit", "send", "stop", "lock",
  "load", "save", "init", "info", "warn", "list", "item", "body", "head",
  "file", "line", "char", "byte", "mark", "mode", "flag", "opts", "args",
  "Math", "Date", "JSON",
  // Common words that appear as local variable names
  "value", "label", "start", "abort", "offset", "memory", "out", "module",
  "react", "server", "files", "error", "match", "token", "index", "input",
  "query", "event", "state", "store", "cache", "config",
  // Common 2-letter words (not minified identifiers)
  "as", "at", "be", "an", "it", "go", "os", "js", "ts", "ws", "re",
  "us", "If", "Do", "or", "in",
  // Single letters used as loop vars / params (unsafe for global rename)
  "a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m",
  "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z",
  "A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M",
  "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z",
  "_", "$",
]);

// Already renamed identifiers
const ALREADY_RENAMED = new Set([
  "emitEvent", "writeDebugLog", "isTruthy", "useAppState", "figures",
  "getSessionId", "useTheme", "trySafeStringify", "resolveFilePath",
  "sendError", "reactMemoCache", "lazyOnce", "updateSettings",
  "getSettings", "ScrollableContent", "getConfigValue",
  "cliMain", "setupCommander", "showSetupScreens", "completeOnboarding",
  "loadManagedSettings", "isDebuggerAttached", "initHeadless", "headlessExports",
  "initApp", "appExports", "webFetchTool", "webFetchInputSchema",
  "webFetchOutputSchema", "checkToolPermissions", "agentLoop",
  "countMessageTokens", "readTool", "microcompact", "autocompactFn",
  "shouldAutocompact", "calculateTokenThresholds", "compactConversation",
  "effectiveContextWindow", "compactThreshold", "partialCompact",
]);

function shouldSkip(id) {
  if (JS_KEYWORDS.has(id)) return true;
  if (COMMON_SHORT.has(id)) return true;
  if (ALREADY_RENAMED.has(id)) return true;
  // Skip purely numeric-looking
  if (/^\d+$/.test(id)) return true;
  return false;
}

// ─── String map (from safe-rename.mjs) ──────────────────────────────────────

function isIdChar(ch) {
  return ch && SAFE_BOUNDARY.test(ch);
}

function buildStringMap(source) {
  const map = new Uint8Array(source.length);
  let i = 0;
  const REGEX_PREV = new Set("=(!|&?:;,+-*%^~<>[{/");

  function isRegexStart() {
    let j = i - 1;
    while (j >= 0 && (source[j] === " " || source[j] === "\t" || source[j] === "\n" || source[j] === "\r")) j--;
    if (j < 0) return true;
    const prev = source[j];
    if (REGEX_PREV.has(prev)) return true;
    if (/[a-z]/.test(prev)) {
      let start = j;
      while (start > 0 && /[a-z]/.test(source[start - 1])) start--;
      const word = source.substring(start, j + 1);
      return ["return", "typeof", "void", "delete", "throw", "new", "case", "in", "instanceof"].includes(word);
    }
    return false;
  }

  function scanRegex() {
    map[i] = 1; i++;
    let inCharClass = false;
    while (i < source.length) {
      if (source[i] === "\\") { map[i] = 1; map[i + 1] = 1; i += 2; continue; }
      if (source[i] === "[") { inCharClass = true; map[i] = 1; i++; continue; }
      if (source[i] === "]") { inCharClass = false; map[i] = 1; i++; continue; }
      if (source[i] === "/" && !inCharClass) {
        map[i] = 1; i++;
        while (i < source.length && /[gimsuy]/.test(source[i])) { map[i] = 1; i++; }
        return;
      }
      if (source[i] === "\n") return;
      map[i] = 1; i++;
    }
  }

  function scanCode() {
    while (i < source.length) {
      const ch = source[i];
      if (ch === "/" && source[i + 1] === "/") {
        while (i < source.length && source[i] !== "\n") { map[i] = 1; i++; }
      } else if (ch === "/" && source[i + 1] === "*") {
        map[i] = 1; map[i + 1] = 1; i += 2;
        while (i < source.length && !(source[i] === "*" && source[i + 1] === "/")) { map[i] = 1; i++; }
        if (i < source.length) { map[i] = 1; map[i + 1] = 1; i += 2; }
      } else if (ch === "/" && isRegexStart()) {
        scanRegex();
      } else if (ch === '"' || ch === "'") {
        map[i] = 1; i++;
        while (i < source.length) {
          if (source[i] === "\\") { map[i] = 1; map[i + 1] = 1; i += 2; continue; }
          if (source[i] === ch) { map[i] = 1; i++; break; }
          map[i] = 1; i++;
        }
      } else if (ch === "`") {
        scanTemplateLiteral();
      } else {
        i++;
      }
    }
  }

  function scanTemplateLiteral() {
    map[i] = 1; i++;
    while (i < source.length) {
      if (source[i] === "\\") { map[i] = 1; map[i + 1] = 1; i += 2; continue; }
      if (source[i] === "$" && source[i + 1] === "{") {
        map[i] = 1; map[i + 1] = 1; i += 2;
        scanInterpolation();
        continue;
      }
      if (source[i] === "`") { map[i] = 1; i++; break; }
      map[i] = 1; i++;
    }
  }

  function scanInterpolation() {
    let depth = 1;
    while (i < source.length && depth > 0) {
      const ch = source[i];
      if (ch === "{") { depth++; i++; }
      else if (ch === "}") {
        depth--;
        if (depth === 0) { map[i] = 1; i++; break; }
        i++;
      } else if (ch === '"' || ch === "'") {
        map[i] = 1; i++;
        while (i < source.length) {
          if (source[i] === "\\") { map[i] = 1; map[i + 1] = 1; i += 2; continue; }
          if (source[i] === ch) { map[i] = 1; i++; break; }
          map[i] = 1; i++;
        }
      } else if (ch === "`") {
        scanTemplateLiteral();
      } else if (ch === "/" && source[i + 1] === "/") {
        while (i < source.length && source[i] !== "\n") { map[i] = 1; i++; }
      } else if (ch === "/" && source[i + 1] === "*") {
        map[i] = 1; map[i + 1] = 1; i += 2;
        while (i < source.length && !(source[i] === "*" && source[i + 1] === "/")) { map[i] = 1; i++; }
        if (i < source.length) { map[i] = 1; map[i + 1] = 1; i += 2; }
      } else if (ch === "/" && isRegexStart()) {
        scanRegex();
      } else {
        i++;
      }
    }
  }

  scanCode();
  return map;
}

// ─── Identifier replacement (from safe-rename.mjs) ─────────────────────────

function isPropertyKey(source, pos, len) {
  let j = pos + len;
  while (j < source.length && (source[j] === " " || source[j] === "\t")) j++;
  if (source[j] !== ":") return false;
  if (source[j + 1] === ":") return false;
  let k = pos - 1;
  while (k >= 0 && (source[k] === " " || source[k] === "\t" || source[k] === "\n" || source[k] === "\r")) k--;
  if (k < 0) return false;
  const prev = source[k];
  if (prev === "{" || prev === "," || prev === "(" || prev === ";") return true;
  return false;
}

function replaceIdentifier(source, oldId, newId) {
  const stringMap = buildStringMap(source);
  let result = "";
  let count = 0;
  let skipped = 0;
  let i = 0;

  while (i < source.length) {
    if (source.substring(i, i + oldId.length) === oldId) {
      const before = i > 0 ? source[i - 1] : "";
      const after = source[i + oldId.length] || "";
      if (!isIdChar(before) && !isIdChar(after)) {
        if (stringMap[i] === 1) {
          skipped++;
        } else if (before === "." || (before === "?" && i > 1 && source[i - 2] === ".")) {
          skipped++;
        } else if (isPropertyKey(source, i, oldId.length)) {
          skipped++;
        } else {
          result += newId;
          count++;
          i += oldId.length;
          continue;
        }
      }
    }
    result += source[i];
    i++;
  }

  return { result, count, skipped };
}

// ─── Scanning ───────────────────────────────────────────────────────────────

function loadSectionFiles() {
  let files = readdirSync(SECTIONS_DIR)
    .filter(f => f.endsWith(".js"))
    .sort()
    .map(f => join(SECTIONS_DIR, f));

  if (ONLY_FILE) {
    files = files.filter(f => f.endsWith(ONLY_FILE));
    if (files.length === 0) {
      console.error(`No file matching "${ONLY_FILE}" found in ${SECTIONS_DIR}`);
      process.exit(1);
    }
  }
  return files;
}

function findDefinitions(lines, id) {
  const escaped = id.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const defs = [];

  const patterns = [
    { re: new RegExp(`^\\s*(?:var|let|const)\\s+${escaped}\\s*=`), type: "var" },
    { re: new RegExp(`^\\s*function\\s+${escaped}\\s*\\(`), type: "function" },
    { re: new RegExp(`^\\s*class\\s+${escaped}\\s*[{(]`), type: "class" },
    { re: new RegExp(`^\\s*(?:var|let|const)\\s+${escaped}\\s*;`), type: "decl" },
  ];

  for (let i = 0; i < lines.length; i++) {
    for (const p of patterns) {
      if (p.re.test(lines[i])) {
        defs.push({
          line: i + 1,
          type: p.type,
          context: lines.slice(i, Math.min(i + 5, lines.length)).map(l => l.trimEnd()),
        });
      }
    }
  }
  return defs;
}

function findUsageExamples(lines, id, maxExamples = 5, stringMap = null) {
  const examples = [];
  const source = lines.join("\n");

  // Build a line offset table for string map lookups
  let lineOffsets = null;
  if (stringMap) {
    lineOffsets = [];
    let offset = 0;
    for (let i = 0; i < lines.length; i++) {
      lineOffsets.push(offset);
      offset += lines[i].length + 1; // +1 for \n
    }
  }

  for (let i = 0; i < lines.length && examples.length < maxExamples * 3; i++) {
    const line = lines[i];
    // Find the identifier as a standalone word (not inside another identifier)
    let pos = 0;
    let found = false;
    while ((pos = line.indexOf(id, pos)) !== -1) {
      const before = pos > 0 ? line[pos - 1] : "";
      const after = line[pos + id.length] || "";
      if (!isIdChar(before) && !isIdChar(after)) {
        // If we have a string map, check this isn't inside a string/comment/regex
        if (stringMap && lineOffsets) {
          const absPos = lineOffsets[i] + pos;
          if (absPos < stringMap.length && stringMap[absPos] === 1) {
            pos++;
            continue;
          }
        }
        found = true;
        break;
      }
      pos++;
    }
    if (!found) continue;

    // Skip definition lines (we show those separately)
    const trimmed = line.trim();
    if (/^(?:var|let|const|function|class)\s/.test(trimmed)) continue;

    examples.push({
      line: i + 1,
      text: trimmed.substring(0, 120),
    });
  }

  // Spread examples evenly across the file
  if (examples.length > maxExamples) {
    const step = Math.floor(examples.length / maxExamples);
    return examples.filter((_, i) => i % step === 0).slice(0, maxExamples);
  }
  return examples;
}

function scanCandidates(files) {
  console.log(`Scanning ${files.length} files for rename candidates...`);
  console.log(`  Identifier length: ${MIN_LEN}-${MAX_LEN}, min occurrences: ${MIN_COUNT}\n`);

  // Load all files once
  const fileData = files.map(f => {
    const src = readFileSync(f, "utf-8");
    return {
      path: f,
      basename: f.split("/").pop(),
      src,
      lines: src.split("\n"),
    };
  });

  // Pass 1: count raw occurrences and find definitions in a single scan
  const globalCounts = {};
  const perFileCounts = {};
  const allDefs = {};   // id → [{ line, type, context, file }]
  const idRe = /\b([a-zA-Z_$][a-zA-Z0-9_$]*)\b/g;

  for (const fd of fileData) {
    // Count occurrences
    let m;
    idRe.lastIndex = 0;
    while ((m = idRe.exec(fd.src)) !== null) {
      const id = m[1];
      if (id.length < MIN_LEN || id.length > MAX_LEN) continue;
      if (shouldSkip(id)) continue;
      globalCounts[id] = (globalCounts[id] || 0) + 1;
      if (!perFileCounts[id]) perFileCounts[id] = {};
      perFileCounts[id][fd.basename] = (perFileCounts[id][fd.basename] || 0) + 1;
    }

    // Find definitions
    for (let i = 0; i < fd.lines.length; i++) {
      const line = fd.lines[i];
      // Quick pre-check to avoid regex for every line
      if (!/^\s*(?:var|let|const|function|class)\s/.test(line)) continue;

      // Check each candidate id that appears on this line
      const defRe = /\b(?:var|let|const)\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*[=;]|function\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\(|class\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*[{(]/g;
      let dm;
      while ((dm = defRe.exec(line)) !== null) {
        const id = dm[1] || dm[2] || dm[3];
        if (!id || id.length < MIN_LEN || id.length > MAX_LEN || shouldSkip(id)) continue;
        const type = dm[1] ? (line.includes("=") ? "var" : "decl") : dm[2] ? "function" : "class";
        if (!allDefs[id]) allDefs[id] = [];
        allDefs[id].push({
          line: i + 1,
          type,
          context: fd.lines.slice(i, Math.min(i + 5, fd.lines.length)).map(l => l.trimEnd()),
          file: fd.basename,
          filePath: fd.path,
        });
      }
    }
  }

  // Pass 2: filter to single-definition candidates meeting threshold
  const candidateIds = [];
  for (const [id, rawCount] of Object.entries(globalCounts)) {
    if (rawCount < MIN_COUNT) continue;
    const defs = allDefs[id];
    if (!defs || defs.length !== 1) continue;
    candidateIds.push(id);
  }

  console.log(`  ${Object.keys(globalCounts).length} unique identifiers, ${candidateIds.length} with single definition`);
  process.stdout.write("  Building string maps for usage examples...");

  // Build string maps per file (lazily cached)
  const stringMapCache = new Map();
  function getStringMap(fd) {
    if (!stringMapCache.has(fd.path)) {
      stringMapCache.set(fd.path, buildStringMap(fd.src));
    }
    return stringMapCache.get(fd.path);
  }

  // Build candidates
  const candidates = [];

  for (const id of candidateIds) {
    const def = allDefs[id][0];
    const defFd = fileData.find(fd => fd.path === def.filePath);
    const usages = defFd ? findUsageExamples(defFd.lines, id, 5, getStringMap(defFd)) : [];

    candidates.push({
      id,
      rawCount: globalCounts[id],
      codeCount: globalCounts[id],
      def,
      usages,
      files: perFileCounts[id],
    });
  }
  console.log(" done.\n");

  // Sort by raw occurrence count (most impactful first)
  candidates.sort((a, b) => b.rawCount - a.rawCount);

  return candidates;
}

// ─── Display ────────────────────────────────────────────────────────────────

const BOLD = "\x1b[1m";
const DIM = "\x1b[2m";
const CYAN = "\x1b[36m";
const GREEN = "\x1b[32m";
const YELLOW = "\x1b[33m";
const RED = "\x1b[31m";
const RESET = "\x1b[0m";

function displayCandidate(candidate, index, total) {
  const { id, codeCount, def, usages, files } = candidate;

  console.log(`\n${"─".repeat(70)}`);
  console.log(`${BOLD}${CYAN}[${index + 1}/${total}]${RESET} ${BOLD}${id}${RESET}  ${DIM}(${codeCount} code occurrences)${RESET}`);
  console.log();

  // Definition
  console.log(`${GREEN}Definition:${RESET} ${def.file}:${def.line} (${def.type})`);
  for (const line of def.context.slice(0, 4)) {
    console.log(`  ${DIM}${line}${RESET}`);
  }

  // File spread
  const fileList = Object.entries(files)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 5)
    .map(([f, c]) => `${f}:${c}`)
    .join(", ");
  console.log(`\n${YELLOW}Files:${RESET} ${fileList}`);

  // Usage examples
  if (usages.length > 0) {
    console.log(`\n${YELLOW}Usage examples:${RESET}`);
    for (const u of usages.slice(0, 4)) {
      console.log(`  ${DIM}L${u.line}: ${u.text}${RESET}`);
    }
  }
}

// ─── Build & test ───────────────────────────────────────────────────────────

function buildAndTest() {
  if (SKIP_TEST) return { ok: true };

  try {
    execSync("node build.mjs", { stdio: "pipe" });
  } catch (e) {
    return { ok: false, stage: "build", error: e.stderr?.toString() || e.message };
  }

  try {
    const out = execSync('CLAUDECODE= node dist/cli.js --version', { stdio: "pipe", timeout: 10000 });
    if (!out.toString().includes("klaudia")) {
      return { ok: false, stage: "version", error: `Unexpected: ${out.toString().trim()}` };
    }
  } catch (e) {
    return { ok: false, stage: "version", error: e.stderr?.toString() || e.message };
  }

  try {
    execSync('CLAUDECODE= node dist/cli.js -p "say ok"', { stdio: "pipe", timeout: 30000 });
  } catch (e) {
    return { ok: false, stage: "print", error: e.stderr?.toString() || e.message };
  }

  return { ok: true };
}

function backupSections() {
  const backupDir = "src/sections/.backup";
  mkdirSync(backupDir, { recursive: true });
  const files = readdirSync(SECTIONS_DIR).filter(f => f.endsWith(".js"));
  for (const f of files) {
    copyFileSync(join(SECTIONS_DIR, f), join(backupDir, f));
  }
  return backupDir;
}

function restoreBackup(backupDir) {
  const files = readdirSync(backupDir).filter(f => f.endsWith(".js"));
  for (const f of files) {
    copyFileSync(join(backupDir, f), join(SECTIONS_DIR, f));
  }
  rmSync(backupDir, { recursive: true });
}

function clearBackup(backupDir) {
  if (existsSync(backupDir)) rmSync(backupDir, { recursive: true });
}

// ─── Interactive prompt ─────────────────────────────────────────────────────

function prompt(rl, question) {
  return new Promise((resolve) => {
    rl.question(question, resolve);
  });
}

function logRename(oldId, newId, count) {
  const entry = JSON.stringify({ ts: new Date().toISOString(), old: oldId, new: newId, count });
  try {
    const existing = existsSync(RENAME_LOG) ? readFileSync(RENAME_LOG, "utf-8") : "";
    writeFileSync(RENAME_LOG, existing + entry + "\n");
  } catch {}
}

async function interactiveLoop(candidates, sectionFiles) {
  const rl = createInterface({
    input: process.stdin,
    output: process.stdout,
  });

  let renamed = 0;
  let skipped = 0;
  let totalReplacements = 0;

  // Skip to --after position
  let startIdx = 0;
  if (AFTER_ID) {
    const idx = candidates.findIndex(c => c.id === AFTER_ID);
    if (idx === -1) {
      console.log(`${RED}Identifier "${AFTER_ID}" not found in candidates.${RESET}`);
      rl.close();
      return;
    }
    startIdx = idx + 1;
    console.log(`Resuming after ${AFTER_ID} (skipping ${startIdx} candidates)`);
  }

  console.log(`\n${BOLD}Interactive Rename${RESET}`);
  console.log(`${candidates.length - startIdx} candidates. For each:`);
  console.log(`  ${GREEN}Type a new name${RESET} to rename`);
  console.log(`  ${DIM}Enter${RESET} to skip`);
  console.log(`  ${DIM}q${RESET} to quit`);
  console.log(`  ${DIM}?${RESET} for more context`);
  if (DRY_RUN) console.log(`  ${YELLOW}DRY RUN mode — no changes will be written${RESET}`);
  if (SKIP_TEST) console.log(`  ${YELLOW}SKIP TEST mode — build+test disabled${RESET}`);

  for (let i = startIdx; i < candidates.length; i++) {
    const c = candidates[i];
    displayCandidate(c, i, candidates.length);

    const answer = await prompt(rl, `\n${BOLD}Rename ${c.id} →${RESET} `);
    const input = answer.trim();

    if (input === "q" || input === "quit") {
      console.log("\nQuitting.");
      break;
    }

    if (input === "" || input === "-") {
      skipped++;
      continue;
    }

    if (input === "?") {
      // Show more context — all usages from all files (with string map filtering)
      for (const file of sectionFiles) {
        const src = readFileSync(file, "utf-8");
        const lines = src.split("\n");
        const smap = buildStringMap(src);
        const examples = findUsageExamples(lines, c.id, 10, smap);
        if (examples.length > 0) {
          console.log(`\n  ${GREEN}${file.split("/").pop()}:${RESET}`);
          for (const u of examples) {
            console.log(`    ${DIM}L${u.line}: ${u.text}${RESET}`);
          }
        }
      }
      // Re-prompt
      i--;
      continue;
    }

    // Validate new name
    if (!/^[a-zA-Z_$][a-zA-Z0-9_$]*$/.test(input)) {
      console.log(`${RED}  Invalid identifier: "${input}"${RESET}`);
      i--;
      continue;
    }

    if (input === c.id) {
      console.log(`${YELLOW}  Same name, skipping.${RESET}`);
      skipped++;
      continue;
    }

    // Check if new name already exists (fast raw count)
    let existingCount = 0;
    const existRe = new RegExp(`\\b${input.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\b`, "g");
    for (const file of sectionFiles) {
      const src = readFileSync(file, "utf-8");
      const matches = src.match(existRe);
      existingCount += matches ? matches.length : 0;
    }
    if (existingCount > 0) {
      console.log(`${YELLOW}  Warning: "${input}" already has ${existingCount} occurrences in codebase.${RESET}`);
      const confirm = await prompt(rl, `  Continue anyway? (y/N) `);
      if (confirm.trim().toLowerCase() !== "y") {
        i--;
        continue;
      }
    }

    // Apply rename
    console.log(`  Applying ${c.id} → ${input}...`);

    let backupDir;
    if (!DRY_RUN) {
      backupDir = backupSections();
    }

    let renameCount = 0;
    for (const file of sectionFiles) {
      const src = readFileSync(file, "utf-8");
      const { result, count } = replaceIdentifier(src, c.id, input);
      if (count > 0) {
        if (!DRY_RUN) writeFileSync(file, result);
        console.log(`    ${file.split("/").pop()}: ${count}`);
        renameCount += count;
      }
    }

    if (renameCount === 0) {
      console.log(`  ${YELLOW}No replacements made.${RESET}`);
      if (backupDir) clearBackup(backupDir);
      continue;
    }

    console.log(`  ${BOLD}Total: ${renameCount} replacements${RESET}`);

    if (!DRY_RUN) {
      process.stdout.write("  Building & testing... ");
      const test = buildAndTest();
      if (!test.ok) {
        console.log(`${RED}FAILED (${test.stage})${RESET}`);
        console.log(`  ${RED}${test.error}${RESET}`);
        console.log("  Rolling back...");
        restoreBackup(backupDir);
        console.log("  Rolled back.");
        continue;
      }
      console.log(`${GREEN}OK${RESET}`);
      clearBackup(backupDir);
      logRename(c.id, input, renameCount);
    } else {
      console.log(`  ${YELLOW}[DRY RUN] Would make ${renameCount} replacements.${RESET}`);
    }

    renamed++;
    totalReplacements += renameCount;
  }

  rl.close();

  console.log(`\n${"─".repeat(70)}`);
  console.log(`${BOLD}Summary:${RESET} ${renamed} renamed, ${skipped} skipped, ${totalReplacements} total replacements`);
}

// ─── Main ───────────────────────────────────────────────────────────────────

const sectionFiles = loadSectionFiles();
const candidates = scanCandidates(sectionFiles);

console.log(`Found ${BOLD}${candidates.length}${RESET} safe rename candidates\n`);

if (SCAN_ONLY) {
  // Just print the table
  console.log("ID".padEnd(8) + "Count".padStart(6) + "  " + "Def".padEnd(30) + "  " + "File");
  console.log("─".repeat(80));
  for (const c of candidates) {
    const defStr = `${c.def.type} @ L${c.def.line}`;
    console.log(
      c.id.padEnd(8) +
      String(c.codeCount).padStart(6) +
      "  " +
      defStr.padEnd(30) +
      "  " +
      c.def.file
    );
  }
} else {
  if (candidates.length === 0) {
    console.log("No candidates found matching criteria.");
    process.exit(0);
  }
  await interactiveLoop(candidates, sectionFiles);
}
