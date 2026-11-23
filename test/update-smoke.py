"""Exercise the published installation layouts against a loopback release/registry."""
import functools
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import threading
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer

ROOT = Path(__file__).resolve().parents[1]
PACKAGES = ROOT / "packaging/npm/packages"


def run(*args, cwd=ROOT, env=None):
    return subprocess.check_output(args, cwd=cwd, env=env, text=True).strip()


def archive(path, files):
    with tarfile.open(path, "w:gz", compresslevel=1) as tar:
        for name, data in files.items():
            if isinstance(data, Path):
                tar.add(data, arcname=name, recursive=False)
            else:
                info = tarfile.TarInfo(name)
                info.size = len(data)
                tar.addfile(info, io.BytesIO(data))


class Handler(SimpleHTTPRequestHandler):
    def do_GET(self):
        # npm percent-encodes the slash in scoped package names.
        self.path = self.path.replace("%2f", "/").replace("%2F", "/")
        super().do_GET()

    def log_message(self, *args):
        pass


def publish_fixture(web, url, platform, binaries):
    for name in ("factile", "@factile/cli", platform):
        versions = {}
        for ver, binary in binaries.items():
            if name == platform:
                meta = {"name": name}
                files = {"package/bin/factile": binary}
            else:
                source = PACKAGES / name.split("/")[-1]
                meta = json.loads((source / "package.json").read_text())
                if name == "factile":
                    meta["optionalDependencies"] = {platform: ver}
                else:
                    meta["dependencies"] = {"factile": ver}
                files = {"package/" + str(p.relative_to(source)): p
                         for p in source.rglob("*.js")}
            meta["version"] = ver
            files["package/package.json"] = json.dumps(meta).encode()
            filename = name.split("/")[-1] + "-" + ver + ".tgz"
            archive(web / filename, files)
            versions[ver] = {**meta, "dist": {"tarball": url + "/" + filename,
                                              "shasum": hashlib.sha1((web / filename).read_bytes()).hexdigest()}}
        doc = web / name
        doc.parent.mkdir(exist_ok=True)
        doc.write_text(json.dumps({"name": name, "dist-tags": {"latest": ver}, "versions": versions}))


def check(web, tmp, url, new_binary):
    version = (ROOT / "VERSION").read_text().strip()
    old = "0.0.1"
    os_name = run("go", "env", "GOOS")
    arch = run("go", "env", "GOARCH")
    old_binary = tmp / "old-factile"
    run("go", "build", "-ldflags", f"-X github.com/factile/factile/pkg/version.Version=v{old} "
        f"-X github.com/factile/factile/internal/selfupdate.latestRelease={url}/latest",
        "-o", old_binary, "./cmd/factile")
    name = f"factile_{version}_{os_name}_{arch}.tar.gz"
    archive(web / name, {"factile": new_binary})
    digest = hashlib.sha256((web / name).read_bytes()).hexdigest()
    (web / "checksums.txt").write_text(f"{digest}  {name}\n")
    (web / "latest").write_text(json.dumps({"tag_name": "v" + version, "assets": [
        {"name": x, "browser_download_url": url + "/" + x} for x in (name, "checksums.txt")]}))
    installed = tmp / "standalone"
    shutil.copy2(old_binary, installed)
    alias = tmp / "ft"
    alias.symlink_to(installed)
    receipt = {"installed_version": "v" + old, "target_version": "v" + version, "updated": True}
    assert json.loads(run(alias, "update", "--json", cwd=tmp)) == receipt
    assert alias.is_symlink() and run(installed, "version", cwd=tmp) == "factile v" + version

    cpu = {"amd64": "x64", "arm64": "arm64"}[arch]
    publish_fixture(web, url, f"@factile/cli-{os_name}-{cpu}", {old: old_binary, version: new_binary})
    env = {**os.environ, "npm_config_registry": url, "npm_config_cache": str(tmp / "cache"),
           "npm_config_userconfig": str(tmp / "npmrc"), "npm_config_fetch_retries": "0"}
    for owner in ("factile", "@factile/cli"):
        for glob in (True, False):
            prefix = Path(tempfile.mkdtemp(dir=tmp))
            if not glob:
                (prefix / "package.json").write_text('{"private": true}')
            run("npm", "install", "--no-audit", "--no-fund", "--prefix", prefix,
                "--global=" + str(glob).lower(), owner + "@" + old, env=env, cwd=prefix)
            bindir = prefix / ("bin" if glob else "node_modules/.bin")
            launcher = bindir / "ft"
            assert run(launcher, "version", cwd=tmp, env=env) == "factile v" + old
            assert json.loads(run(launcher, "update", "--json", cwd=tmp, env=env)) == receipt
            assert run(bindir / "factile", "version", cwd=tmp, env=env) == "factile v" + version
            package = prefix / ("lib/node_modules" if glob else "node_modules") / owner / "package.json"
            assert json.loads(package.read_text())["version"] == version
            assert json.loads(run(launcher, "update", "--json", cwd=tmp, env=env))["updated"] is False


if __name__ == "__main__":
    with tempfile.TemporaryDirectory() as directory:
        tmp = Path(directory)
        web = tmp / "registry"
        web.mkdir()
        with ThreadingHTTPServer(("127.0.0.1", 0), functools.partial(Handler, directory=web)) as server:
            threading.Thread(target=server.serve_forever, daemon=True).start()
            try:
                check(web, tmp, f"http://127.0.0.1:{server.server_port}", Path(sys.argv[1]).resolve())
            finally:
                server.shutdown()
