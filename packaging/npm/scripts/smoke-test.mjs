#!/usr/bin/env node
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync, spawnSync } from "node:child_process";

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
    packagesDir: "",
  };

  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--packages-dir") {
      const value = argv[i + 1];
      if (!value) {
        throw new Error("--packages-dir requires a value");
      }
      args.packagesDir = path.resolve(value);
      i += 1;
      continue;
    }
    throw new Error(`unknown argument: ${arg}`);
  }

  if (!args.packagesDir) {
    throw new Error("--packages-dir is required");
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

function runBinResult(workspace, command, args) {
  const executable = process.platform === "win32" ? `${command}.cmd` : command;
  const result = spawnSync(path.join(workspace, "node_modules", ".bin", executable), args, {
    cwd: workspace,
    encoding: "utf8",
  });
  if (result.error && result.status === null) {
    throw result.error;
  }
  return result;
}

function runBin(workspace, command, args) {
  const result = runBinResult(workspace, command, args);
  if (result.status !== 0) {
    throw new Error(
      `${command} ${args.join(" ")} failed with status ${result.status}:\n${result.stderr}`
    );
  }
  return result.stdout;
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
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

    const canonicalReadme = fs.readFileSync(
      path.join(workspace, "node_modules", "factile", "README.md"),
      "utf8"
    );
    assert(
      canonicalReadme.includes("Normal repository onboarding and repair use one command") &&
        canonicalReadme.includes("factile init"),
      "canonical package README does not present factile init as one-command onboarding"
    );
    const aliasReadmePath = path.join(
      workspace,
      "node_modules",
      "@factile",
      "cli",
      "README.md"
    );
    if (fs.existsSync(aliasReadmePath)) {
      const aliasReadme = fs.readFileSync(aliasReadmePath, "utf8");
      assert(
        aliasReadme.includes("factile init") && aliasReadme.includes("repeat repair command"),
        "scoped package README does not present factile init as the repeat repair command"
      );
    }

    runBin(workspace, "factile", ["version"]);
    runBin(workspace, "ft", ["version"]);

    const missingWorkspace = runBinResult(workspace, "factile", ["status", "--json"]);
    assert(
      missingWorkspace.status === 4 && missingWorkspace.stderr.includes("no_active_workspace"),
      `clean install did not report no_active_workspace: ${missingWorkspace.stderr}`
    );
    assert(
      !fs.existsSync(path.join(workspace, ".factile")),
      "workspace-free status created .factile state"
    );

    const detachedBundle = fs.mkdtempSync(path.join(os.tmpdir(), "factile-detached-bundle-"));
    try {
      fs.writeFileSync(
        path.join(detachedBundle, "factile.toml"),
        'version = 2\n\n[bundle]\nname = "detached-smoke"\ntitle = "Detached smoke bundle"\n'
      );
      fs.writeFileSync(
        path.join(detachedBundle, "overview.md"),
        "---\ntype: Reference\ntitle: Detached overview\n---\n\n# Detached overview\n"
      );
      const inspected = JSON.parse(
        runBin(workspace, "factile", ["bundle", "inspect", detachedBundle, "--json"])
      );
      assert(inspected.plausible_okf === true, "detached bundle inspection was not plausible OKF");
    } finally {
      fs.rmSync(detachedBundle, { recursive: true, force: true });
    }

    runBin(workspace, "factile", ["init", "--json"]);
    assert(fs.existsSync(path.join(workspace, "factile.toml")), "init did not create factile.toml");
    assert(
      fs.existsSync(path.join(workspace, "docs", "factile.toml")),
      "init did not create docs/factile.toml"
    );
    assert(fs.existsSync(path.join(workspace, "docs", "index.md")), "init did not create docs/index.md");
    assert(
      fs.existsSync(path.join(workspace, "docs", "overview.md")),
      "init did not create docs/overview.md"
    );
    assert(
      fs.readFileSync(path.join(workspace, ".gitignore"), "utf8").includes("/.factile/"),
      "init did not ignore workspace-local .factile state"
    );
    assert(!fs.existsSync(path.join(workspace, ".factile")), "init eagerly created .factile state");

    const status = JSON.parse(runBin(workspace, "factile", ["status", "--json"]));
    assert(status.workspace.workspace_dir === workspace, "status reported the wrong workspace_dir");
    assert(
      status.workspace.root_bundle_dir === path.join(workspace, "docs"),
      "status reported the wrong root_bundle_dir"
    );
    assert(
      status.workspace.state_dir === path.join(workspace, ".factile"),
      "status reported the wrong state_dir"
    );
    runBin(workspace, "factile", ["list", "/", "--json"]);
    runBin(workspace, "factile", ["read", "/overview", "--json"]);
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
    const platformTarball = npmPack(path.join(args.packagesDir, target.sourceDir), tarballDir);
    const factileTarball = npmPack(path.join(args.packagesDir, "packages/factile"), tarballDir);
    const aliasTarball = npmPack(path.join(args.packagesDir, "packages/cli"), tarballDir);

    installAndCheck("factile", [platformTarball, factileTarball]);
    installAndCheck("cli", [platformTarball, factileTarball, aliasTarball]);
  } finally {
    fs.rmSync(tarballDir, { recursive: true, force: true });
    fs.rmSync(npmCache, { recursive: true, force: true });
  }
}

main();
