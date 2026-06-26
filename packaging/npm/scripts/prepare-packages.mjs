#!/usr/bin/env node
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

import { platformTargets } from "./package-targets.mjs";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const packagingRoot = path.resolve(scriptDir, "..");
const repoRoot = path.resolve(packagingRoot, "..", "..");

function parseArgs(argv) {
  const args = {
    build: false,
    dist: "",
    out: "",
    version: "",
  };

  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--build") {
      args.build = true;
      continue;
    }
    if (arg === "--dist" || arg === "--out" || arg === "--version") {
      const value = argv[i + 1];
      if (!value) {
        throw new Error(`${arg} requires a value`);
      }
      args[arg.slice(2)] = value;
      i += 1;
      continue;
    }
    throw new Error(`unknown argument: ${arg}`);
  }

  if (!args.out) {
    throw new Error("--out is required");
  }
  if (!args.version) {
    throw new Error("--version is required");
  }
  if (args.build && args.dist) {
    throw new Error("use either --build or --dist, not both");
  }
  if (!args.build && !args.dist) {
    throw new Error("one of --build or --dist is required");
  }

  args.version = normalizeVersion(args.version);
  args.out = path.resolve(args.out);
  if (args.dist) {
    args.dist = path.resolve(args.dist);
  }
  return args;
}

function normalizeVersion(version) {
  const value = version.trim().replace(/^v/, "");
  if (!/^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(value)) {
    throw new Error(`invalid npm version: ${version}`);
  }
  return value;
}

function assertSafeOutDir(outDir) {
  const resolved = path.resolve(outDir);
  if (resolved === "/" || resolved === repoRoot || resolved === packagingRoot) {
    throw new Error(`refusing to clear unsafe output directory: ${resolved}`);
  }
}

function copyDir(source, destination) {
  fs.cpSync(source, destination, {
    recursive: true,
    filter: (entry) => !entry.includes(`${path.sep}node_modules${path.sep}`),
  });
}

function copyRepoMetadata(destination) {
  for (const file of ["LICENSE", "NOTICE", "TRADEMARKS.md"]) {
    fs.copyFileSync(path.join(repoRoot, file), path.join(destination, file));
  }
}

function readPackageJSON(packageDir) {
  return JSON.parse(fs.readFileSync(path.join(packageDir, "package.json"), "utf8"));
}

function writePackageJSON(packageDir, value) {
  fs.writeFileSync(
    path.join(packageDir, "package.json"),
    `${JSON.stringify(value, null, 2)}\n`
  );
}

function stagePackage(sourceRelative, destination, version, mutate) {
  copyDir(path.join(packagingRoot, sourceRelative), destination);
  copyRepoMetadata(destination);

  const pkg = readPackageJSON(destination);
  pkg.version = version;
  mutate(pkg);
  writePackageJSON(destination, pkg);
}

function platformReadme(target) {
  return `# ${target.packageName}

Native Factile CLI binary package for ${target.nodePlatform}/${target.nodeArch}.

This package is installed automatically as an optional dependency of the
canonical \`factile\` npm package.
`;
}

function buildBinary(target, destination, version) {
  const binDir = path.join(destination, "bin");
  fs.mkdirSync(binDir, { recursive: true });
  const output = path.join(binDir, target.binaryName);
  const ldflags = [
    "-s",
    "-w",
    `-X github.com/factile/factile/pkg/version.Version=v${version}`,
  ].join(" ");

  execFileSync("go", ["build", "-trimpath", "-ldflags", ldflags, "-o", output, "./cmd/factile"], {
    cwd: repoRoot,
    env: {
      ...process.env,
      CGO_ENABLED: "0",
      GOCACHE: process.env.GOCACHE || "/tmp/factile-go-build-cache",
      GOMODCACHE: process.env.GOMODCACHE || "/tmp/factile-go-mod-cache",
      GOOS: target.goos,
      GOARCH: target.goarch,
    },
    stdio: "inherit",
  });

  if (target.goos !== "windows") {
    fs.chmodSync(output, 0o755);
  }
}

function copyBinaryFromDist(target, destination, distDir) {
  const archive = findArchive(target, distDir);
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "factile-npm-archive-"));
  try {
    if (archive.endsWith(".tar.gz")) {
      execFileSync("tar", ["-xzf", archive, "-C", tempDir], { stdio: "inherit" });
    } else if (archive.endsWith(".zip")) {
      execFileSync("unzip", ["-q", archive, "-d", tempDir], { stdio: "inherit" });
    } else {
      throw new Error(`unsupported archive type: ${archive}`);
    }

    const binary = findFile(tempDir, target.binaryName);
    const binDir = path.join(destination, "bin");
    fs.mkdirSync(binDir, { recursive: true });
    const output = path.join(binDir, target.binaryName);
    fs.copyFileSync(binary, output);
    if (target.goos !== "windows") {
      fs.chmodSync(output, 0o755);
    }
  } finally {
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
}

function findArchive(target, distDir) {
  const entries = fs.readdirSync(distDir);
  const matches = entries
    .filter((entry) => entry.includes(`_${target.goos}_${target.goarch}`))
    .filter((entry) => entry.endsWith(".tar.gz") || entry.endsWith(".zip"))
    .sort();

  if (matches.length === 0) {
    throw new Error(`missing GoReleaser archive for ${target.goos}/${target.goarch}`);
  }
  return path.join(distDir, matches[0]);
}

function findFile(root, fileName) {
  const entries = fs.readdirSync(root, { withFileTypes: true });
  for (const entry of entries) {
    const fullPath = path.join(root, entry.name);
    if (entry.isDirectory()) {
      const nested = findFile(fullPath, fileName);
      if (nested) {
        return nested;
      }
    }
    if (entry.isFile() && entry.name === fileName) {
      return fullPath;
    }
  }
  return "";
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  assertSafeOutDir(args.out);
  fs.rmSync(args.out, { recursive: true, force: true });
  fs.mkdirSync(args.out, { recursive: true });

  for (const target of platformTargets) {
    const destination = path.join(args.out, target.sourceDir);
    stagePackage(target.sourceDir, destination, args.version, () => {});
    fs.writeFileSync(path.join(destination, "README.md"), platformReadme(target));
    if (args.build) {
      buildBinary(target, destination, args.version);
    } else {
      copyBinaryFromDist(target, destination, args.dist);
    }
  }

  const optionalDependencies = Object.fromEntries(
    platformTargets.map((target) => [target.packageName, args.version])
  );

  stagePackage("packages/factile", path.join(args.out, "packages/factile"), args.version, (pkg) => {
    pkg.optionalDependencies = optionalDependencies;
  });

  stagePackage("packages/cli", path.join(args.out, "packages/cli"), args.version, (pkg) => {
    pkg.dependencies = {
      factile: args.version,
    };
  });

  console.log(`prepared npm packages in ${args.out}`);
}

main();
