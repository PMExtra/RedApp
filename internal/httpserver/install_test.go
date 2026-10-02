package httpserver

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	app "github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/testutil"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testSHA(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func TestEnterpriseInstallerThroughRedAppAndHashFailure(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("Linux x64 CLI 集成场景")
	}
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for path, body := range map[string]string{"bin/codex": "#!/bin/sh\necho 'codex-cli 0.159.2'\n", "bin/codex-code-mode-host": "#!/bin/sh\nexit 0\n", "codex-path/rg": "#!/bin/sh\nexit 0\n", "codex-resources/bwrap": "#!/bin/sh\nexit 0\n", "codex-package.json": "{}"} {
		if e := tarWriter.WriteHeader(&tar.Header{Name: path, Mode: 0755, Size: int64(len(body))}); e != nil {
			t.Fatal(e)
		}
		tarWriter.Write([]byte(body))
	}
	tarWriter.Close()
	gzipWriter.Close()
	archive := buffer.Bytes()
	name := "codex-package-x86_64-unknown-linux-musl.tar.gz"
	manifest := []byte(fmt.Sprintf("%s  %s\n", testSHA(archive), name))
	for _, bad := range []bool{false, true} {
		t.Run(fmt.Sprint("corrupt=", bad), func(t *testing.T) {
			var base string
			c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "latest") || strings.HasSuffix(r.URL.Path, "release.json"):
					json.NewEncoder(w).Encode(app.Release{Tag: "rust-v0.159.2", Assets: []app.Asset{{Name: name, Digest: "sha256:" + testSHA(archive), URL: base + "/releases/0.159.2/" + name}, {Name: "codex-package_SHA256SUMS", Digest: "sha256:" + testSHA(manifest), URL: base + "/releases/0.159.2/codex-package_SHA256SUMS"}}})
				case strings.HasSuffix(r.URL.Path, "SHA256SUMS"):
					w.Write(manifest)
				default:
					if bad {
						w.Write(append(append([]byte(nil), archive...), []byte("corrupted")...))
					} else {
						w.Write(archive)
					}
				}
			}))
			base = c.Base.String()
			handler, _, _ := newTestServer(t, c)
			enterprise := startTestServer(t, handler)
			response, e := http.Get(enterprise.URL + "/openai/codex/install.sh")
			if e != nil {
				t.Fatal(e)
			}
			script, e := io.ReadAll(response.Body)
			response.Body.Close()
			if e != nil || response.StatusCode != http.StatusOK {
				t.Fatalf("installer response: status=%d, error=%v, body=%s", response.StatusCode, e, script)
			}
			clientRoot := t.TempDir()
			scriptPath := filepath.Join(clientRoot, "install.sh")
			os.WriteFile(scriptPath, script, 0600)
			binDir := filepath.Join(clientRoot, "bin")
			os.Mkdir(binDir, 0700)
			cmd := exec.Command("sh", scriptPath)
			cmd.Env = append(os.Environ(), "CODEX_HOME="+filepath.Join(clientRoot, "codex-home"), "CODEX_INSTALL_DIR="+binDir, "CODEX_RELEASE=latest", "CODEX_NON_INTERACTIVE=1", "CODEX_INSTALLER_USE_RELEASES_OPENAI_COM=0", "PATH="+binDir+":/usr/bin:/bin")
			output, e := cmd.CombinedOutput()
			_, statErr := os.Stat(filepath.Join(binDir, "codex"))
			if bad {
				if e == nil || statErr == nil {
					t.Fatalf("损坏制品仍安装成功: %s", output)
				}
			} else {
				if e != nil || statErr != nil {
					t.Fatalf("企业服务安装失败: %v\n%s", e, output)
				}
			}
			if _, e = os.Stat(filepath.Join(clientRoot, "codex-home/packages/standalone/auto-update-version")); !os.IsNotExist(e) {
				t.Fatal("marker 未删除")
			}
		})
	}
}
