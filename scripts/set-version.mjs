#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, "..");
const versionPattern = /^\d+\.\d+\.\d+$/;
const newVersion = process.argv[2] ?? "";

if (!versionPattern.test(newVersion)) {
  throw new Error("usage: node scripts/set-version.mjs X.Y.Z");
}

const versionFile = path.join(repoRoot, "VERSION");
const oldVersion = fs.readFileSync(versionFile, "utf8").trim();
if (!versionPattern.test(oldVersion)) {
  throw new Error(`invalid SemVer in VERSION: ${oldVersion}`);
}
if (oldVersion === newVersion) {
  throw new Error(`VERSION is already ${newVersion}`);
}

const updates = new Map();

function planTextUpdate(relativePath, oldText, newText) {
  const file = path.join(repoRoot, relativePath);
  const current = fs.readFileSync(file, "utf8");
  if (!current.includes(oldText)) {
    throw new Error(`${relativePath} does not contain expected version ${oldText}`);
  }
  updates.set(file, current.replaceAll(oldText, newText));
}

function packageFiles(root) {
  const files = [];
  for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
    const entryPath = path.join(root, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === "dist" || entry.name === "node_modules") {
        continue;
      }
      files.push(...packageFiles(entryPath));
    } else if (entry.name === "package.json") {
      files.push(entryPath);
    }
  }
  return files;
}

planTextUpdate(
  "pkg/version/version.go",
  `defaultVersion = "v${oldVersion}"`,
  `defaultVersion = "v${newVersion}"`,
);
planTextUpdate("README.md", `v${oldVersion}`, `v${newVersion}`);
planTextUpdate(
  "packaging/npm/README.md",
  `--version ${oldVersion}`,
  `--version ${newVersion}`,
);

for (const file of packageFiles(path.join(repoRoot, "packaging", "npm"))) {
  const pkg = JSON.parse(fs.readFileSync(file, "utf8"));
  if (pkg.version !== oldVersion) {
    throw new Error(`${path.relative(repoRoot, file)} has version ${pkg.version}, expected ${oldVersion}`);
  }
  pkg.version = newVersion;

  for (const field of ["dependencies", "optionalDependencies"]) {
    for (const [name, version] of Object.entries(pkg[field] ?? {})) {
      if (name === "factile" || name.startsWith("@factile/cli")) {
        if (version !== oldVersion) {
          throw new Error(
            `${path.relative(repoRoot, file)} has ${name}@${version}, expected ${oldVersion}`,
          );
        }
        pkg[field][name] = newVersion;
      }
    }
  }

  for (const [name, command] of Object.entries(pkg.scripts ?? {})) {
    pkg.scripts[name] = command.replaceAll(oldVersion, newVersion);
  }

  updates.set(file, `${JSON.stringify(pkg, null, 2)}\n`);
}

updates.set(versionFile, `${newVersion}\n`);
for (const [file, contents] of updates) {
  fs.writeFileSync(file, contents);
}

console.log(`updated version ${oldVersion} -> ${newVersion}`);
