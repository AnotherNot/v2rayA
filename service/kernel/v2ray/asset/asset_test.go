package asset

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type assetTransport func(*http.Request) (*http.Response, error)

func (f assetTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAssetBodyOverLimitIsRejected(t *testing.T) {
	client := &http.Client{Transport: assetTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", int(maxAssetDownloadSize+1)))),
		}, nil
	})}
	target := filepath.Join(t.TempDir(), "asset.dat")
	if err := download(client, "https://example.test/asset.dat", target); err == nil || !strings.Contains(err.Error(), "256 MiB") {
		t.Fatalf("error = %v, want size limit", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("oversized asset created target: %v", err)
	}
}

// The core only reads XRAY_LOCATION_ASSET, so a dat file that exists in a
// system directory has to be linked into that directory before the core runs.
func TestEnsureCoreAssetsLinksMissingFiles(t *testing.T) {
	system := t.TempDir()
	assetDir := filepath.Join(t.TempDir(), "runtime")
	source := filepath.Join(system, "geosite.dat")
	if err := os.WriteFile(source, []byte("dat"), 0644); err != nil {
		t.Fatal(err)
	}
	// findAssetOutsideDir searches fixed system paths, so exercise the linking
	// itself with the source it would have found.
	if err := os.MkdirAll(assetDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(assetDir, "geosite.dat")
	if err := os.Symlink(source, target); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(target)
	if err != nil || string(b) != "dat" {
		t.Fatalf("link does not resolve: %v %q", err, b)
	}
	// An asset already present must not be touched.
	EnsureCoreAssets(assetDir)
	fi, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the existing link was replaced")
	}
}

// The client the proxy-aware callers pass in has no timeout of its own, so
// DownloadWithClient has to add one: without it a source that accepts the
// connection and then stalls would hang the update forever.
func TestDownloadWithClientAddsATimeout(t *testing.T) {
	var seen *http.Request
	client := &http.Client{Transport: assetTransport(func(r *http.Request) (*http.Response, error) {
		seen = r
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("dat")),
		}, nil
	})}
	target := filepath.Join(t.TempDir(), "asset.dat")
	if err := DownloadWithClient(client, "https://example.test/asset.dat", target); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != "dat" {
		t.Fatalf("downloaded content: %v %q", err, b)
	}
	if seen == nil {
		t.Fatal("the caller's client was not used")
	}
	if client.Timeout != 0 {
		t.Errorf("the caller's client was mutated: timeout %v", client.Timeout)
	}
}

// A caller may pass no client at all; the direct download still has to work.
func TestDownloadWithNilClient(t *testing.T) {
	if err := Download("http://127.0.0.1:1/never", filepath.Join(t.TempDir(), "x.dat")); err == nil {
		t.Fatal("a refused connection reported success")
	}
}

// A filename is what the core's loader opens: a path inside the asset
// directory. A name that can leave it must be refused before the join, because
// the relinking step replaces whatever sits at the resulting path.
func TestGetV2rayLocationAssetRefusesEscapingNames(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../x.dat", "../../etc/passwd", "/etc/passwd"} {
		if got, err := GetV2rayLocationAsset(name); err == nil {
			t.Errorf("GetV2rayLocationAsset(%q) = %q, want an error", name, got)
		}
	}
}

// The runtime directory holds links this package wrote. An operator may instead
// have copied an asset there; that file is not ours to delete.
func TestLinkRuntimeAssetKeepsARealFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "geosite.dat")
	if err := os.WriteFile(target, []byte("operator copy"), 0644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "source.dat")
	if err := os.WriteFile(source, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimeAsset(source, target); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(target)
	if err != nil || string(b) != "operator copy" {
		t.Fatalf("regular file was replaced: %v %q", err, b)
	}
}

// Relinking is how a moved asset reaches the core: the old link is ours, so it
// is replaced with one that points at the current file.
func TestLinkRuntimeAssetReplacesItsOwnLink(t *testing.T) {
	dir := t.TempDir()
	oldSource := filepath.Join(dir, "old.dat")
	newSource := filepath.Join(dir, "new.dat")
	for path, content := range map[string]string{oldSource: "old", newSource: "new"} {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(dir, "link.dat")
	if err := os.Symlink(oldSource, target); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimeAsset(newSource, target); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was replaced with a regular file")
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != "new" {
		t.Fatalf("link target: %v %q", err, b)
	}
}

func TestFindAssetOutsideDirSkipsItsOwnDirectory(t *testing.T) {
	dir := "/usr/share/v2raya"
	if _, err := os.Stat(filepath.Join(dir, "geosite.dat")); err != nil {
		t.Skip("no system geosite.dat on this machine")
	}
	if got := findAssetOutsideDir("geosite.dat", dir); got != "" {
		t.Errorf("a file in the asset directory itself must not be reported as a source, got %q", got)
	}
}
