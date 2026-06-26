#!/usr/bin/env node
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";

import { packageOrder } from "./package-targets.mjs";

const npmCache = fs.mkdtempSync(path.join(os.tmpdir(), "factile-npm-publish-cache-"));

function npmEnv() {
  return {
    ...process.env,
    NPM_CONFIG_CACHE: process.env.NPM_CONFIG_CACHE || npmCache,
  };
}

function parseArgs(argv) {
  const args = {
    dryRun: false,
    root: "",
  };

  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--dry-run") {
      args.dryRun = true;
      continue;
    }
    if (arg === "--root") {
      const value = argv[i + 1];
      if (!value) {
        throw new Error("--root requires a value");
      }
      args.root = path.resolve(value);
      i += 1;
      continue;
    }
    throw new Error(`unknown argument: ${arg}`);
  }

  if (!args.root) {
    throw new Error("--root is required");
  }
  return args;
}

function readPackageName(packageDir) {
  const pkg = JSON.parse(fs.readFileSync(path.join(packageDir, "package.json"), "utf8"));
  return pkg.name;
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  try {
    for (const entry of packageOrder) {
      const packageDir = path.join(args.root, entry.stagedDir);
      const packageName = readPackageName(packageDir);
      if (packageName !== entry.packageName) {
        throw new Error(`unexpected package at ${packageDir}: ${packageName}`);
      }

      const publishArgs = ["publish", "--provenance", "--access", "public"];
      if (args.dryRun) {
        publishArgs.push("--dry-run");
      }

      console.log(`publishing ${packageName}`);
      execFileSync("npm", publishArgs, {
        cwd: packageDir,
        env: npmEnv(),
        stdio: "inherit",
      });
    }
  } finally {
    fs.rmSync(npmCache, { recursive: true, force: true });
  }
}

main();
