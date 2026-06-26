"use strict";

const { spawnSync } = require("node:child_process");
const path = require("node:path");

const targets = {
  "linux-x64": {
    packageName: "@factile/cli-linux-x64",
    binary: "bin/factile",
  },
  "linux-arm64": {
    packageName: "@factile/cli-linux-arm64",
    binary: "bin/factile",
  },
  "darwin-x64": {
    packageName: "@factile/cli-darwin-x64",
    binary: "bin/factile",
  },
  "darwin-arm64": {
    packageName: "@factile/cli-darwin-arm64",
    binary: "bin/factile",
  },
  "win32-x64": {
    packageName: "@factile/cli-win32-x64",
    binary: "bin/factile.exe",
  },
};

function targetKey() {
  return `${process.platform}-${process.arch}`;
}

function resolveBinary() {
  const target = targets[targetKey()];
  if (!target) {
    throw new Error(`unsupported platform for factile npm package: ${targetKey()}`);
  }

  let packagePath;
  try {
    packagePath = require.resolve(`${target.packageName}/package.json`);
  } catch (error) {
    throw new Error(
      `missing native Factile package ${target.packageName}; reinstall with optional dependencies enabled`
    );
  }

  return path.join(path.dirname(packagePath), target.binary);
}

function run(argv = process.argv.slice(2)) {
  let binary;
  try {
    binary = resolveBinary();
  } catch (error) {
    console.error(error.message);
    process.exit(1);
  }

  const result = spawnSync(binary, argv, { stdio: "inherit" });
  if (result.error) {
    console.error(`failed to run ${binary}: ${result.error.message}`);
    process.exit(1);
  }
  if (result.signal) {
    console.error(`factile exited due to signal ${result.signal}`);
    process.exit(1);
  }
  process.exit(result.status === null ? 1 : result.status);
}

module.exports = {
  resolveBinary,
  run,
};
