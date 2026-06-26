export const platformTargets = [
  {
    id: "linux-x64",
    packageName: "@factile/cli-linux-x64",
    sourceDir: "platform/linux-x64",
    goos: "linux",
    goarch: "amd64",
    nodePlatform: "linux",
    nodeArch: "x64",
    binaryName: "factile",
  },
  {
    id: "linux-arm64",
    packageName: "@factile/cli-linux-arm64",
    sourceDir: "platform/linux-arm64",
    goos: "linux",
    goarch: "arm64",
    nodePlatform: "linux",
    nodeArch: "arm64",
    binaryName: "factile",
  },
  {
    id: "darwin-x64",
    packageName: "@factile/cli-darwin-x64",
    sourceDir: "platform/darwin-x64",
    goos: "darwin",
    goarch: "amd64",
    nodePlatform: "darwin",
    nodeArch: "x64",
    binaryName: "factile",
  },
  {
    id: "darwin-arm64",
    packageName: "@factile/cli-darwin-arm64",
    sourceDir: "platform/darwin-arm64",
    goos: "darwin",
    goarch: "arm64",
    nodePlatform: "darwin",
    nodeArch: "arm64",
    binaryName: "factile",
  },
  {
    id: "win32-x64",
    packageName: "@factile/cli-win32-x64",
    sourceDir: "platform/win32-x64",
    goos: "windows",
    goarch: "amd64",
    nodePlatform: "win32",
    nodeArch: "x64",
    binaryName: "factile.exe",
  },
];

export const packageOrder = [
  ...platformTargets.map((target) => ({
    kind: "platform",
    id: target.id,
    packageName: target.packageName,
    stagedDir: `platform/${target.id}`,
  })),
  {
    kind: "main",
    id: "factile",
    packageName: "factile",
    stagedDir: "packages/factile",
  },
  {
    kind: "alias",
    id: "cli",
    packageName: "@factile/cli",
    stagedDir: "packages/cli",
  },
];

export function currentTarget() {
  return platformTargets.find(
    (target) =>
      target.nodePlatform === process.platform && target.nodeArch === process.arch
  );
}
