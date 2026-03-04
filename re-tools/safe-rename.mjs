#!/usr/bin/env node
// Safe identifier rename with collision detection, build, and test.
// Usage:
//   node tools/safe-rename.mjs <old> <new> [--dry-run]
//   node tools/safe-rename.mjs --batch renames.json [--dry-run]
//
// Single rename: renames <old> → <new> across all section files,
// checks for collisions, builds, and runs smoke tests.
//
// Batch mode: reads a JSON file with an array of [old, new] pairs,
// applies each rename sequentially with collision checks.
//
// Collision checks:
//   1. New name already exists as an identifier in the codebase
//   2. Old name appears inside string literals or regex patterns
//      where word-boundary detection may fail (e.g., char classes)
//
// On failure, rolls back all changes.

import { readFileSync, writeFileSync, readdirSync, copyFileSync, mkdirSync, rmSync, existsSync } from "fs";
import { join } from "path";
import { execSync } from "child_process";

const SECTIONS_DIR = "src/sections";
const SAFE_BOUNDARY = /[a-zA-Z0-9_$\u0080-\uFFFF]/;

function isIdChar(ch) {
  return ch && SAFE_BOUNDARY.test(ch);
}

// Track whether a position is inside a string/comment/regex literal.
// map[i] === 0 means code, map[i] === 1 means string/comment/regex.
// Handles template literal interpolation: ${...} inside backticks is code.
// Handles regex literals: /pattern/flags after operator/keyword context.
function buildStringMap(source) {
  const map = new Uint8Array(source.length); // 0 = code
  let i = 0;

  // Characters that can precede a regex literal (the preceding non-whitespace char).
  const REGEX_PREV = new Set("=(!|&?:;,+-*%^~<>[{/");

  // Check if `/` at position i is the start of a regex literal.
  // Look back for the preceding non-whitespace character.
  function isRegexStart() {
    let j = i - 1;
    while (j >= 0 && (source[j] === " " || source[j] === "\t" || source[j] === "\n" || source[j] === "\r")) j--;
    if (j < 0) return true; // start of file
    const prev = source[j];
    if (REGEX_PREV.has(prev)) return true;
    // Also check for keyword endings: return, typeof, void, delete, throw, new, case, in, instanceof
    // Simple check: if prev is a letter, look at the word
    if (/[a-z]/.test(prev)) {
      let start = j;
      while (start > 0 && /[a-z]/.test(source[start - 1])) start--;
      const word = source.substring(start, j + 1);
      return ["return", "typeof", "void", "delete", "throw", "new", "case", "in", "instanceof"].includes(word);
    }
    return false;
  }

  // Scan a regex literal: /pattern/flags
  function scanRegex() {
    map[i] = 1; i++; // opening /
    let inCharClass = false;
    while (i < source.length) {
      if (source[i] === "\\") {
        map[i] = 1; map[i + 1] = 1; i += 2; continue; // escape
      }
      if (source[i] === "[") { inCharClass = true; map[i] = 1; i++; continue; }
      if (source[i] === "]") { inCharClass = false; map[i] = 1; i++; continue; }
      if (source[i] === "/" && !inCharClass) {
        map[i] = 1; i++; // closing /
        // Consume flags
        while (i < source.length && /[gimsuy]/.test(source[i])) { map[i] = 1; i++; }
        return;
      }
      if (source[i] === "\n") return; // unterminated regex, bail
      map[i] = 1; i++;
    }
  }

  function scanCode() {
    while (i < source.length) {
      const ch = source[i];
      if (ch === "/" && source[i + 1] === "/") {
        // Line comment — skip to end of line
        while (i < source.length && source[i] !== "\n") { map[i] = 1; i++; }
      } else if (ch === "/" && source[i + 1] === "*") {
        // Block comment
        map[i] = 1; map[i + 1] = 1; i += 2;
        while (i < source.length && !(source[i] === "*" && source[i + 1] === "/")) { map[i] = 1; i++; }
        if (i < source.length) { map[i] = 1; map[i + 1] = 1; i += 2; }
      } else if (ch === "/" && isRegexStart()) {
        // Regex literal
        scanRegex();
      } else if (ch === '"' || ch === "'") {
        // Regular string
        map[i] = 1; i++;
        while (i < source.length) {
          if (source[i] === "\\") { map[i] = 1; map[i + 1] = 1; i += 2; continue; }
          if (source[i] === ch) { map[i] = 1; i++; break; }
          map[i] = 1; i++;
        }
      } else if (ch === "`") {
        // Template literal — need to handle ${...} interpolation
        scanTemplateLiteral();
      } else {
        i++;
      }
    }
  }

  function scanTemplateLiteral() {
    map[i] = 1; i++; // opening backtick
    while (i < source.length) {
      if (source[i] === "\\") {
        map[i] = 1; map[i + 1] = 1; i += 2; continue;
      }
      if (source[i] === "$" && source[i + 1] === "{") {
        // Interpolation — mark ${ as string, then parse contents as code
        map[i] = 1; map[i + 1] = 1; i += 2;
        scanInterpolation();
        continue;
      }
      if (source[i] === "`") {
        map[i] = 1; i++; break; // closing backtick
      }
      map[i] = 1; i++;
    }
  }

  // Scan inside ${...} — this is code, but we need to track brace depth
  // and handle nested strings/template literals.
  function scanInterpolation() {
    let depth = 1;
    while (i < source.length && depth > 0) {
      const ch = source[i];
      if (ch === "{") {
        depth++; i++;
      } else if (ch === "}") {
        depth--;
        if (depth === 0) { map[i] = 1; i++; break; } // closing } is part of template syntax
        i++;
      } else if (ch === '"' || ch === "'") {
        // String inside interpolation
        map[i] = 1; i++;
        while (i < source.length) {
          if (source[i] === "\\") { map[i] = 1; map[i + 1] = 1; i += 2; continue; }
          if (source[i] === ch) { map[i] = 1; i++; break; }
          map[i] = 1; i++;
        }
      } else if (ch === "`") {
        // Nested template literal inside interpolation
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
        i++; // regular code character — stays as 0
      }
    }
  }

  scanCode();
  return map;
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
        // Skip if inside a string/comment/regex
        if (stringMap[i] === 1) {
          skipped++;
        // Skip property access: obj.n, obj?.n
        } else if (before === "." || (before === "?" && i > 1 && source[i - 2] === ".")) {
          skipped++;
        // Skip object property key: { n: value } or { ..., n: value }
        // Detect by checking if followed by ":" (not "::") after optional whitespace.
        // But allow ternary: n ? a : b (preceded by different context).
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

// Check if the identifier at position `pos` (with length `len`) is an object property key.
// Pattern: identifier followed by ":" (not "::") with optional whitespace.
// Exclude: variable declarations (var n =), function params, standalone statements.
// Heuristic: look at what precedes the identifier — if it's after "{", ",", or newline
// in an object-like context, it's likely a property key.
function isPropertyKey(source, pos, len) {
  // Check what follows: must be ":" (not "::")
  let j = pos + len;
  while (j < source.length && (source[j] === " " || source[j] === "\t")) j++;
  if (source[j] !== ":") return false;
  if (source[j + 1] === ":") return false; // :: is not property assignment

  // Check what precedes: skip whitespace/newlines
  let k = pos - 1;
  while (k >= 0 && (source[k] === " " || source[k] === "\t" || source[k] === "\n" || source[k] === "\r")) k--;
  if (k < 0) return false;
  const prev = source[k];

  // After { or , or ; at start of object literal or after another property
  // Also after ( for destructuring patterns like ({ n: alias })
  if (prev === "{" || prev === "," || prev === "(" || prev === ";") return true;

  return false;
}

// Check if the old name appears in contexts where boundary detection fails.
// Specifically catches: regex char class ranges like "q-uy" where "-" is not
// an identifier char but the replacement would break the regex.
function findCollisions(source, filename, oldId) {
  const warnings = [];
  const lines = source.split("\n");

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    let pos = 0;

    while ((pos = line.indexOf(oldId, pos)) !== -1) {
      const before = pos > 0 ? line[pos - 1] : "";
      const after = line[pos + oldId.length] || "";

      // Only check replacement sites (where boundary check passes)
      if (!isIdChar(before) && !isIdChar(after)) {
        // Pattern: preceded by "-" (regex char class range like "a-uy")
        // This is the specific collision pattern that breaks regexes
        if (before === "-") {
          // Confirm we're likely inside a string containing a regex char class
          const beforeStr = line.substring(0, pos);
          if (/\[/.test(beforeStr) && /\\d|\\w|\\s|A-Z|a-z|0-9/.test(beforeStr)) {
            warnings.push({
              file: filename,
              line: i + 1,
              type: "regex_range",
              context: line.substring(Math.max(0, pos - 25), pos + oldId.length + 10).trim(),
            });
          }
        }
      }
      pos++;
    }
  }

  return warnings;
}

// Check if new name already exists as an identifier
function checkNewNameExists(files, newId) {
  const existing = [];
  for (const file of files) {
    const source = readFileSync(file, "utf-8");
    const { count } = replaceIdentifier(source, newId, "__PROBE__");
    if (count > 0) {
      existing.push({ file, count });
    }
  }
  return existing;
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
  rmSync(backupDir, { recursive: true });
}

function buildAndTest() {
  try {
    execSync("node build.mjs", { stdio: "pipe" });
  } catch (e) {
    return { ok: false, stage: "build", error: e.stderr?.toString() || e.message };
  }

  try {
    const out = execSync('CLAUDECODE= node dist/cli.js --version', { stdio: "pipe", timeout: 10000 });
    const version = out.toString().trim();
    if (!version.includes("klaudia")) {
      return { ok: false, stage: "version", error: `Unexpected version: ${version}` };
    }
  } catch (e) {
    return { ok: false, stage: "version", error: e.stderr?.toString() || e.message };
  }

  try {
    const out = execSync('CLAUDECODE= node dist/cli.js -p "say ok"', { stdio: "pipe", timeout: 30000 });
    const response = out.toString().trim();
    if (response.length === 0) {
      return { ok: false, stage: "print", error: "Empty response from -p mode" };
    }
  } catch (e) {
    return { ok: false, stage: "print", error: e.stderr?.toString() || e.message };
  }

  return { ok: true };
}

function applyRename(files, oldId, newId, dryRun) {
  let totalCount = 0;
  const results = [];

  // 1. Check for collisions
  for (const file of files) {
    const source = readFileSync(file, "utf-8");
    const warnings = findCollisions(source, file, oldId);
    if (warnings.length > 0) {
      for (const w of warnings) {
        console.warn(`  ⚠ COLLISION in ${w.file}:${w.line} (${w.type}): ...${w.context}...`);
      }
      return { ok: false, reason: "collision", warnings };
    }
  }

  // 2. Check if new name already exists
  const existing = checkNewNameExists(files, newId);
  if (existing.length > 0) {
    console.warn(`  ⚠ New name "${newId}" already exists:`);
    for (const e of existing) {
      console.warn(`    ${e.file}: ${e.count} occurrences`);
    }
    // This is a warning, not a blocker — the user might want this
    // (e.g., renaming to match an existing convention)
  }

  // 3. Apply rename
  for (const file of files) {
    const source = readFileSync(file, "utf-8");
    const { result, count } = replaceIdentifier(source, oldId, newId);
    if (count > 0) {
      results.push({ file, count });
      if (!dryRun) {
        writeFileSync(file, result);
      }
      totalCount += count;
    }
  }

  return { ok: true, totalCount, results };
}

// --- Main ---

const args = process.argv.slice(2);
const dryRun = args.includes("--dry-run");
const batchIdx = args.indexOf("--batch");

const sectionFiles = readdirSync(SECTIONS_DIR)
  .filter(f => f.endsWith(".js"))
  .sort()
  .map(f => join(SECTIONS_DIR, f));

if (batchIdx !== -1) {
  // Batch mode
  const batchFile = args[batchIdx + 1];
  if (!batchFile) {
    console.error("Usage: node tools/safe-rename.mjs --batch <renames.json> [--dry-run]");
    process.exit(1);
  }

  const renames = JSON.parse(readFileSync(batchFile, "utf-8"));
  console.log(`Batch: ${renames.length} renames from ${batchFile}${dryRun ? " (DRY RUN)" : ""}`);

  let backupDir;
  if (!dryRun) {
    backupDir = backupSections();
    console.log(`Backup created at ${backupDir}`);
  }

  let failed = false;
  let totalReplacements = 0;

  for (const [oldId, newId] of renames) {
    process.stdout.write(`  ${oldId} → ${newId}: `);
    const result = applyRename(sectionFiles, oldId, newId, dryRun);

    if (!result.ok) {
      console.log(`BLOCKED (${result.reason})`);
      failed = true;
      break;
    }

    if (result.totalCount === 0) {
      console.log("no matches");
    } else {
      console.log(`${result.totalCount} replacements`);
      totalReplacements += result.totalCount;
    }
  }

  if (failed) {
    if (backupDir) {
      console.log("\nRolling back...");
      restoreBackup(backupDir);
      console.log("Rolled back to pre-batch state.");
    }
    process.exit(1);
  }

  if (!dryRun && totalReplacements > 0) {
    console.log(`\nTotal: ${totalReplacements} replacements. Building and testing...`);
    const test = buildAndTest();
    if (!test.ok) {
      console.error(`\n✗ Test failed at ${test.stage}: ${test.error}`);
      console.log("Rolling back...");
      restoreBackup(backupDir);
      console.log("Rolled back to pre-batch state.");
      process.exit(1);
    }
    clearBackup(backupDir);
    console.log("✓ Build and tests passed!");
  } else if (dryRun) {
    console.log(`\n[DRY RUN] ${totalReplacements} total replacements would be made.`);
  } else {
    console.log("\nNo replacements needed.");
    if (backupDir) clearBackup(backupDir);
  }
} else {
  // Single rename mode
  const filteredArgs = args.filter(a => a !== "--dry-run");
  const [oldId, newId] = filteredArgs;

  if (!oldId || !newId) {
    console.error("Usage: node tools/safe-rename.mjs <old> <new> [--dry-run]");
    console.error("       node tools/safe-rename.mjs --batch <renames.json> [--dry-run]");
    process.exit(1);
  }

  console.log(`Rename: ${oldId} → ${newId}${dryRun ? " (DRY RUN)" : ""}`);

  let backupDir;
  if (!dryRun) {
    backupDir = backupSections();
  }

  const result = applyRename(sectionFiles, oldId, newId, dryRun);

  if (!result.ok) {
    console.error(`\n✗ Blocked: ${result.reason}`);
    if (backupDir) clearBackup(backupDir);
    process.exit(1);
  }

  if (result.totalCount === 0) {
    console.log("No matches found.");
    if (backupDir) clearBackup(backupDir);
    process.exit(0);
  }

  for (const r of result.results) {
    console.log(`  ${r.file}: ${r.count}`);
  }
  console.log(`Total: ${result.totalCount} replacements`);

  if (!dryRun) {
    console.log("\nBuilding and testing...");
    const test = buildAndTest();
    if (!test.ok) {
      console.error(`\n✗ Test failed at ${test.stage}: ${test.error}`);
      console.log("Rolling back...");
      restoreBackup(backupDir);
      console.log("Rolled back.");
      process.exit(1);
    }
    clearBackup(backupDir);
    console.log("✓ Build and tests passed!");
  } else {
    console.log(`\n[DRY RUN] Would make ${result.totalCount} replacements.`);
  }
}
