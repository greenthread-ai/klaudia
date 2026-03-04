import { readFileSync, writeFileSync, mkdirSync, readdirSync, watch } from "fs";
import { join } from "path";

const isWatch = process.argv.includes("--watch");

// Section files in concatenation order
const SECTIONS_DIR = "src/sections";
const OUT_FILE = "dist/cli.js";

function getSectionFiles() {
  return readdirSync(SECTIONS_DIR)
    .filter((f) => f.endsWith(".js"))
    .sort() // 00-, 01-, 02-... ensures correct order
    .map((f) => join(SECTIONS_DIR, f));
}

function buildBundle() {
  const files = getSectionFiles();
  const parts = files.map((f) => readFileSync(f, "utf-8"));

  mkdirSync("dist", { recursive: true });
  writeFileSync(OUT_FILE, parts.join(""));

  console.log(`Build complete: ${OUT_FILE} (${files.length} sections concatenated)`);
}

if (isWatch) {
  buildBundle();
  console.log(`Watching ${SECTIONS_DIR}/ for changes...`);
  watch(SECTIONS_DIR, { recursive: true }, (eventType, filename) => {
    if (filename?.endsWith(".js")) {
      console.log(`\n${filename} changed, rebuilding...`);
      try {
        buildBundle();
      } catch (e) {
        console.error(`Build error: ${e.message}`);
      }
    }
  });
} else {
  buildBundle();
}
