package llamacpp

import "testing"

func TestAssetPatternFor(t *testing.T) {
	cases := []struct {
		goos, goarch string
		wantPattern  string
		wantExt      string
		wantOK       bool
	}{
		{"darwin", "arm64", "macos-arm64", ".tar.gz", true},
		{"darwin", "amd64", "macos-x64", ".tar.gz", true},
		{"linux", "amd64", "ubuntu-x64", ".tar.gz", true},
		{"linux", "arm64", "ubuntu-arm64", ".tar.gz", true},
		{"linux", "s390x", "ubuntu-s390x", ".tar.gz", true},
		{"windows", "amd64", "win-cpu-x64", ".zip", true},
		{"windows", "arm64", "win-cpu-arm64", ".zip", true},
		{"linux", "386", "", "", false},
		{"freebsd", "amd64", "", "", false},
	}

	for _, c := range cases {
		pattern, ext, ok := assetPatternFor(c.goos, c.goarch)
		if ok != c.wantOK || pattern != c.wantPattern || ext != c.wantExt {
			t.Errorf("assetPatternFor(%q, %q) = (%q, %q, %v), want (%q, %q, %v)",
				c.goos, c.goarch, pattern, ext, ok, c.wantPattern, c.wantExt, c.wantOK)
		}
	}
}

func TestFindAsset(t *testing.T) {
	pattern, ext, ok := AssetPattern()
	if !ok {
		t.Skip("no prebuilt llama-server for this platform")
	}

	wantName := "llama-b9659-bin-" + pattern + ext
	r := &Release{Tag: "b9659", Assets: map[string]string{
		wantName: "https://example.com/asset",
	}}

	name, url, ok := r.FindAsset()
	if !ok {
		t.Fatalf("FindAsset did not find expected asset %q in %v", wantName, r.Assets)
	}
	if name != wantName {
		t.Errorf("name = %q, want %q", name, wantName)
	}
	if url != "https://example.com/asset" {
		t.Errorf("url = %q, want %q", url, "https://example.com/asset")
	}
}

func TestFindAsset_NotPresent(t *testing.T) {
	if _, _, ok := AssetPattern(); !ok {
		t.Skip("no prebuilt llama-server for this platform")
	}

	r := &Release{Tag: "b9659", Assets: map[string]string{
		"some-other-asset.tar.gz": "https://example.com/asset",
	}}

	if _, _, ok := r.FindAsset(); ok {
		t.Errorf("expected FindAsset to report not found")
	}
}

func TestSafeJoin(t *testing.T) {
	if _, err := safeJoin("/tmp/dest", "../evil"); err == nil {
		t.Error("expected error for path traversal entry")
	}
	if _, err := safeJoin("/tmp/dest", "nested/../../evil"); err == nil {
		t.Error("expected error for nested path traversal entry")
	}
	if _, err := safeJoin("/tmp/dest", "ok/file.txt"); err != nil {
		t.Errorf("unexpected error for safe path: %v", err)
	}
}
