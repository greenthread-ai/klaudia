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

function replaceIdentifier(source, oldId, newId) {
  let result = "";
  let count = 0;
  let i = 0;

  while (i < source.length) {
    if (source.substring(i, i + oldId.length) === oldId) {
      const before = i > 0 ? source[i - 1] : "";
      const after = source[i + oldId.length] || "";
      if (!isIdChar(before) && !isIdChar(after)) {
        result += newId;
        count++;
        i += oldId.length;
        continue;
      }
    }
    result += source[i];
    i++;
  }

  return { result, count };
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
