"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

function npm(args, cwd) {
  const options = { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] };
  // npm.cmd needs a shell. Invoke npm's JS entry point directly on Windows.
  if (process.platform === "win32") {
    const cli = path.join(path.dirname(process.execPath), "node_modules", "npm", "bin", "npm-cli.js");
    return execFileSync(process.execPath, [cli, ...args], options).trim();
  }
  return execFileSync("npm", args, options).trim();
}

function updatePackage(argv) {
  const json = argv.includes("--json") || argv.some((arg, i) => arg === "--format" && argv[i + 1] === "json");
  try {
    const launcher = fs.realpathSync(process.argv[1]);
    const packageDir = path.dirname(path.dirname(launcher));
    let { name, version: installed } = JSON.parse(fs.readFileSync(path.join(packageDir, "package.json"), "utf8"));
    if (name !== "factile" && name !== "@factile/cli") throw new Error("unrecognized Factile npm launcher");
    const modules = name === "factile" ? path.dirname(packageDir) : path.resolve(packageDir, "../..");
    if (path.basename(modules) !== "node_modules") throw new Error("the Factile launcher is outside an npm installation");
    const base = path.dirname(modules);
    // npm installs global shims under prefix/bin on Unix and prefix on Windows.
    // Use the launcher's actual prefix even when it differs from npm's default.
    const windows = process.platform === "win32";
    const global = windows
      ? fs.existsSync(path.join(base, "factile.cmd"))
      : path.basename(base) === "lib" && ["factile", "ft"].some(bin => {
          try { return fs.realpathSync(path.resolve(base, "../bin", bin)) === launcher; }
          catch { return false; }
        });
    const prefix = global && !windows ? path.dirname(base) : base;
    if (!global) {
      const project = JSON.parse(fs.readFileSync(path.join(prefix, "package.json"), "utf8"));
      const deps = { ...project.dependencies, ...project.devDependencies, ...project.optionalDependencies };
      // npm can hoist the canonical launcher for the scoped alias.
      if (!deps[name] && deps["@factile/cli"]) name = "@factile/cli";
      if (!deps[name]) throw new Error("Factile is a transitive dependency; update its owning npm package");
    }
    const published = JSON.parse(npm(["view", name + "@latest", "version", "--json"], prefix));
    const target = Array.isArray(published) && published.length === 1 ? published[0] : published;
    if (typeof target !== "string" || !/^\d+\.\d+\.\d+$/.test(target)) throw new Error("npm returned an invalid stable Factile version");
    const parts = installed.split(/[+-]/)[0].split(".").map(Number);
    const latest = target.split(".").map(Number);
    const differing = parts.findIndex((part, i) => part !== latest[i]);
    const updated = differing === -1 ? installed.includes("-") : parts[differing] < latest[differing];
    if (updated) {
      npm(["install", "--prefix", prefix, "--global=" + global, "--include=optional", "--ignore-scripts", "--no-audit", "--no-fund", name + "@" + target], prefix);
      const version = JSON.parse(execFileSync(process.execPath, [launcher, "version", "--json"], { cwd: prefix, encoding: "utf8" }));
      if (version.version !== "v" + target) throw new Error("updated npm launcher did not report v" + target);
    }
    const result = { installed_version: "v" + installed, target_version: "v" + target, updated };
    if (json) console.log(JSON.stringify(result));
    else if (!argv.includes("--quiet")) console.log(updated
      ? `Updated Factile from v${installed} to v${target}.`
      : `Factile v${installed} is up to date (latest release v${target}).`);
  } catch (error) {
    const message = "update Factile: " + (error.stderr?.toString().trim() || error.message);
    if (json) console.error(JSON.stringify({ error: { code: "update_failed", message } }));
    else console.error(message);
    process.exitCode = 1;
  }
}

module.exports = { updatePackage };
