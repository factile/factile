package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func releaseArchive(t *testing.T, platform, name string, contents []byte) []byte {
	t.Helper()
	var data bytes.Buffer
	if platform == "windows" {
		w := zip.NewWriter(&data)
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(contents); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		g := gzip.NewWriter(&data)
		w := tar.NewWriter(g)
		if err := w.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(contents))}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(contents); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := g.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return data.Bytes()
}

func TestReleaseInstall(t *testing.T) {
	for _, tc := range []struct {
		label, platform, installed, failure string
		wantUpdate                          bool
	}{
		{"tar", "linux", "v0.9.0", "", true},
		{"zip", "windows", "v0.9.0", "", true},
		{"current", "darwin", "v0.10.0", "", false},
		{"ahead", "linux", "v0.11.0", "", false},
		{"release candidate", "linux", "v0.10.0-rc.1", "", true},
		{"bad checksum", "linux", "v0.9.0", "checksum", false},
		{"missing manifest entry", "windows", "v0.9.0", "manifest", false},
		{"missing asset", "linux", "v0.9.0", "asset", false},
		{"download failure", "linux", "v0.9.0", "download", false},
		{"unwritable installation", "linux", "v0.9.0", "permission", false},
		{"wrong entry", "linux", "v0.9.0", "entry", false},
		{"unexpected prerelease", "linux", "v0.9.0", "prerelease", false},
	} {
		t.Run(tc.label, func(t *testing.T) {
			const tag = "v0.10.0"
			binary := "factile"
			archive := "factile_0.10.0_" + tc.platform + "_amd64.tar.gz"
			if tc.platform == "windows" {
				binary = "factile.exe"
				archive = "factile_0.10.0_windows_amd64.zip"
			}
			name := binary
			if tc.failure == "entry" {
				name = "../factile"
			}
			data := releaseArchive(t, tc.platform, name, []byte("new executable"))
			sum := sha256.Sum256(data)
			if tc.failure == "checksum" {
				sum[0] ^= 1
			}
			mux := http.NewServeMux()
			server := httptest.NewServer(mux)
			defer server.Close()
			mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
				assets := []map[string]string{{"name": "checksums.txt", "browser_download_url": server.URL + "/checksums"}}
				if tc.failure != "asset" {
					assets = append(assets, map[string]string{"name": archive, "browser_download_url": server.URL + "/archive"})
				}
				json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "assets": assets, "prerelease": tc.failure == "prerelease"})
			})
			mux.HandleFunc("/checksums", func(w http.ResponseWriter, r *http.Request) {
				if tc.installed == tag || tc.installed == "v0.11.0" {
					t.Error("unnecessary asset request")
				}
				if tc.failure == "manifest" {
					return
				}
				fmt.Fprintf(w, "%x  %s\n", sum, archive)
			})
			mux.HandleFunc("/archive", func(w http.ResponseWriter, r *http.Request) {
				if tc.failure == "download" {
					http.Error(w, "unavailable", 503)
					return
				}
				w.Write(data)
			})
			exe := filepath.Join(t.TempDir(), binary)
			if err := os.WriteFile(exe, []byte("old executable"), 0755); err != nil {
				t.Fatal(err)
			}
			if tc.failure == "permission" {
				if runtime.GOOS == "windows" || os.Geteuid() == 0 {
					t.Skip("requires POSIX directory permissions and an unprivileged user")
				}
				dir := filepath.Dir(exe)
				if err := os.Chmod(dir, 0555); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(dir, 0755) })
			}
			before, err := os.Stat(exe)
			if err != nil {
				t.Fatal(err)
			}
			got, err := install(context.Background(), server.Client(), server.URL+"/latest", exe, tc.installed, tc.platform, "amd64")
			if (err != nil) != (tc.failure != "") || got.Updated != tc.wantUpdate {
				t.Fatalf("result=%+v err=%v", got, err)
			}
			if err == nil && (got.Installed != tc.installed || got.Target != tag) {
				t.Fatal(got)
			}
			content, err := os.ReadFile(exe)
			if err != nil {
				t.Fatal(err)
			}
			wanted := "old executable"
			if tc.wantUpdate {
				wanted = "new executable"
			}
			if string(content) != wanted {
				t.Fatalf("got %s", content)
			}
			if !tc.wantUpdate {
				after, err := os.Stat(exe)
				if err != nil || !os.SameFile(before, after) {
					t.Fatalf("original file was replaced: %v", err)
				}
			}
		})
	}
}

func TestExecutableSizeAndManifest(t *testing.T) {
	var out bytes.Buffer
	if err := copyExecutable(&out, strings.NewReader("")); err == nil {
		t.Fatal("accepted empty executable")
	}
	if _, err := checksum([]byte("zz  factile.zip"), "factile.zip"); err == nil {
		t.Fatal("accepted invalid digest")
	}
}
