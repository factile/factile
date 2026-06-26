#!/usr/bin/env node
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";

import { currentTarget } from "./package-targets.mjs";

const npmCache = fs.mkdtempSync(path.join(os.tmpdir(), "factile-npm-cache-"));

function npmEnv() {
  return {
    ...process.env,
    NPM_CONFIG_CACHE: process.env.NPM_CONFIG_CACHE || npmCache,
  };
}

function parseArgs(argv) {
  const args = {
    root: "",
  };

  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
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

function npmPack(packageDir, destination) {
  const before = new Set(fs.readdirSync(destination));
  execFileSync("npm", ["pack", "--pack-destination", destination], {
    cwd: packageDir,
    env: npmEnv(),
    stdio: ["ignore", "ignore", "inherit"],
  });
  const created = fs
    .readdirSync(destination)
    .filter((entry) => entry.endsWith(".tgz") && !before.has(entry))
    .sort();
  if (created.length !== 1) {
    throw new Error(`npm pack did not return a tarball filename for ${packageDir}`);
  }
  return path.join(destination, created[0]);
}

function runBin(workspace, command, args) {
  const executable = process.platform === "win32" ? `${command}.cmd` : command;
  execFileSync(path.join(workspace, "node_modules", ".bin", executable), args, {
    cwd: workspace,
    stdio: "inherit",
  });
}

function installAndCheck(name, tarballs) {
  const workspace = fs.mkdtempSync(path.join(os.tmpdir(), `factile-npm-${name}-`));
  try {
    const dependencies = {};
    for (const tarball of tarballs) {
      const packageName = packageNameFromTarball(tarball);
      dependencies[packageName] = `file:${tarball}`;
    }
    fs.writeFileSync(
      path.join(workspace, "package.json"),
      `${JSON.stringify({ private: true, dependencies }, null, 2)}\n`
    );
    execFileSync("npm", ["install", "--offline", "--no-audit", "--no-fund", "--omit=optional"], {
      cwd: workspace,
      env: npmEnv(),
      stdio: "inherit",
    });
    runBin(workspace, "factile", ["version"]);
    runBin(workspace, "ft", ["version"]);
  } finally {
    fs.rmSync(workspace, { recursive: true, force: true });
  }
}

function packageNameFromTarball(tarball) {
  const fileName = path.basename(tarball);
  if (fileName.startsWith("factile-cli-linux-x64-")) {
    return "@factile/cli-linux-x64";
  }
  if (fileName.startsWith("factile-cli-linux-arm64-")) {
    return "@factile/cli-linux-arm64";
  }
  if (fileName.startsWith("factile-cli-darwin-x64-")) {
    return "@factile/cli-darwin-x64";
  }
  if (fileName.startsWith("factile-cli-darwin-arm64-")) {
    return "@factile/cli-darwin-arm64";
  }
  if (fileName.startsWith("factile-cli-win32-x64-")) {
    return "@factile/cli-win32-x64";
  }
  if (fileName.startsWith("factile-cli-")) {
    return "@factile/cli";
  }
  if (fileName.startsWith("factile-")) {
    return "factile";
  }
  throw new Error(`could not infer package name from tarball: ${tarball}`);
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  const target = currentTarget();
  if (!target) {
    throw new Error(`unsupported smoke-test platform: ${process.platform}/${process.arch}`);
  }

  const tarballDir = fs.mkdtempSync(path.join(os.tmpdir(), "factile-npm-tarballs-"));
  try {
    const platformTarball = npmPack(path.join(args.root, target.sourceDir), tarballDir);
    const factileTarball = npmPack(path.join(args.root, "packages/factile"), tarballDir);
    const aliasTarball = npmPack(path.join(args.root, "packages/cli"), tarballDir);

    installAndCheck("factile", [platformTarball, factileTarball]);
    installAndCheck("cli", [platformTarball, factileTarball, aliasTarball]);
  } finally {
    fs.rmSync(tarballDir, { recursive: true, force: true });
    fs.rmSync(npmCache, { recursive: true, force: true });
  }
}

main();
