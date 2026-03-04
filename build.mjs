import { build, context } from "esbuild";

const isWatch = process.argv.includes("--watch");

const buildOptions = {
  entryPoints: ["src/cli.js"],
  bundle: false, // Source is already a self-contained bundle
  platform: "node",
  target: "node18",
  format: "esm",
  outfile: "dist/cli.js",
  define: {
    "process.env.KLAUDIA_VERSION": '"0.1.0"',
  },
};

if (isWatch) {
  const ctx = await context(buildOptions);
  await ctx.watch();
  console.log("Watching for changes...");
} else {
  await build(buildOptions);
  console.log("Build complete: dist/cli.js");
}
