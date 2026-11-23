// Package selfupdate installs a verified standalone Factile release.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/factile/factile/pkg/storage"
)

// Kept as a build variable so release smoke tests can use a private fixture server.
var latestRelease = "https://api.github.com/repos/factile/factile/releases/latest"

const maxArchive = 128 << 20
const maxBinary = 256 << 20

var stableTag = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type Result struct {
	Installed string `json:"installed_version"`
	Target    string `json:"target_version"`
	Updated   bool   `json:"updated"`
}

func Run(ctx context.Context, installed string) (Result, error) {
	exe, err := os.Executable()
	if err != nil {
		return Result{}, err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return Result{}, err
	}
	// A directly invoked npm platform binary still belongs to its wrapper.
	pkg := filepath.Dir(filepath.Dir(exe))
	if filepath.Base(filepath.Dir(pkg)) == "@factile" && strings.HasPrefix(filepath.Base(pkg), "cli-") {
		return Result{}, errors.New("use the npm-installed factile or ft launcher to update this package")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	return install(ctx, http.DefaultClient, latestRelease, exe, installed, runtime.GOOS, runtime.GOARCH)
}

func install(ctx context.Context, client *http.Client, endpoint, exe, installed, platform, arch string) (Result, error) {
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	data, err := download(ctx, client, endpoint, 2<<20)
	if err != nil {
		return Result{}, err
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return Result{}, fmt.Errorf("read latest Factile release: %w", err)
	}
	if release.Draft || release.Prerelease || !stableTag.MatchString(release.Tag) {
		return Result{}, fmt.Errorf("invalid stable Factile release %q", release.Tag)
	}
	result := Result{Installed: installed, Target: release.Tag}
	if alreadyCurrent(installed, release.Tag) {
		return result, nil
	}
	stat, err := os.Stat(exe)
	if err != nil {
		return result, err
	}
	archive := fmt.Sprintf("factile_%s_%s_%s", release.Tag[1:], platform, arch)
	binary := "factile"
	if platform == "windows" {
		archive += ".zip"
		binary += ".exe"
	} else {
		archive += ".tar.gz"
	}
	assets := make(map[string]string)
	for _, asset := range release.Assets {
		assets[asset.Name] = asset.URL
	}
	if assets[archive] == "" || assets["checksums.txt"] == "" {
		return result, fmt.Errorf("release %s is missing %s or checksums.txt", release.Tag, archive)
	}
	manifest, err := download(ctx, client, assets["checksums.txt"], 1<<20)
	if err != nil {
		return result, err
	}
	expected, err := checksum(manifest, archive)
	if err != nil {
		return result, err
	}
	data, err = download(ctx, client, assets[archive], maxArchive)
	if err != nil {
		return result, err
	}
	digest := sha256.Sum256(data)
	if !bytes.Equal(digest[:], expected) {
		return result, fmt.Errorf("SHA-256 checksum mismatch for %s", archive)
	}

	stage, err := os.CreateTemp(filepath.Dir(exe), ".factile-update-*")
	if err != nil {
		return result, err
	}
	defer os.Remove(stage.Name())
	err = extract(stage, binary, data)
	if err == nil {
		err = stage.Chmod(stat.Mode().Perm())
	}
	if err == nil {
		err = stage.Sync()
	}
	if err = errors.Join(err, stage.Close()); err != nil {
		return result, err
	}
	err = storage.WithFileLocks([]string{exe}, func() error {
		current, err := os.Stat(exe)
		if err != nil {
			return err
		}
		if !os.SameFile(stat, current) {
			return errors.New("Factile changed during the download; run factile update again")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return replaceExecutable(stage.Name(), exe)
	})
	result.Updated = err == nil
	return result, err
}

// Compare the numeric components even when a local build is ahead of the release.
func alreadyCurrent(installed, target string) bool {
	base, _, _ := strings.Cut(installed, "+")
	base, _, pre := strings.Cut(base, "-")
	a, b := stableTag.FindStringSubmatch(base), stableTag.FindStringSubmatch(target)
	if a == nil || b == nil {
		return false
	}
	for i := 1; i < len(a); i++ {
		if len(a[i]) != len(b[i]) {
			return len(a[i]) > len(b[i])
		}
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return !pre
}

func download(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download exceeds size limit: %s", url)
	}
	return data, err
}

func checksum(manifest []byte, name string) ([]byte, error) {
	for _, line := range strings.Split(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			sum, err := hex.DecodeString(fields[0])
			if err == nil && len(sum) == sha256.Size {
				return sum, nil
			}
			break
		}
	}
	return nil, fmt.Errorf("missing or invalid SHA-256 checksum for %s", name)
}

// Read only the named regular file. Archive paths never become filesystem paths.
func extract(out io.Writer, binary string, data []byte) error {
	if binary == "factile.exe" {
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return err
		}
		for _, f := range z.File {
			if f.Name == binary && f.Mode().IsRegular() {
				r, err := f.Open()
				if err != nil {
					return err
				}
				defer r.Close()
				return copyExecutable(out, r)
			}
		}
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return err
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if header.Name == binary && header.Typeflag == tar.TypeReg {
				return copyExecutable(out, tr)
			}
		}
	}
	return fmt.Errorf("release archive is missing %s", binary)
}

func copyExecutable(out io.Writer, in io.Reader) error {
	n, err := io.Copy(out, io.LimitReader(in, maxBinary+1))
	if err != nil {
		return err
	}
	if n == 0 || n > maxBinary {
		return fmt.Errorf("invalid release binary size %d", n)
	}
	return nil
}

func replaceExecutable(stage, exe string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(stage, exe)
	}
	// Windows allows renaming a running image but can deny overwriting it.
	backup := exe + ".factile-update-old"
	if err := os.Rename(exe, backup); err != nil {
		return err
	}
	if err := os.Rename(stage, exe); err != nil {
		return errors.Join(err, os.Rename(backup, exe))
	}
	// A still-running image can remain here until the next upgrade reuses the path.
	_ = os.Remove(backup)
	return nil
}
