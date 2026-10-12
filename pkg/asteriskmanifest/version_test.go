package asteriskmanifest_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/go-tangra/go-tangra-asterisk/v4/pkg/asteriskmanifest"
)

var update = flag.Bool("update", false, "rewrite testdata/manifest.sha256 for the current Version")

const pinFile = "testdata/manifest.sha256"

// The gateway keeps one manifest per module and accepts a different one only
// with a higher Version; the same Version with changed content is refused
// (manifest_drift) until every old instance's lease expires. The pin ties
// the manifest's content to its Version: changing the manifest (routes,
// exposes, nav, abilities, permissions) without bumping Version fails here.
// After bumping Version: go test ./pkg/asteriskmanifest -run Pinned -update
func TestManifestVersionPinned(t *testing.T) {
	m, err := asteriskmanifest.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	got := asteriskmanifest.Version + " " + hex.EncodeToString(sum[:])
	if *update {
		if err := os.WriteFile(pinFile, []byte(got+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	b, err := os.ReadFile(pinFile)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	want := strings.TrimSpace(string(b))
	pinnedVersion, _, _ := strings.Cut(want, " ")
	switch {
	case got == want:
	case pinnedVersion == asteriskmanifest.Version:
		t.Fatalf("the manifest changed but Version is still %s: bump asteriskmanifest.Version, then run go test ./pkg/asteriskmanifest -run Pinned -update", asteriskmanifest.Version)
	default:
		t.Fatalf("Version bumped to %s: refresh the pin with go test ./pkg/asteriskmanifest -run Pinned -update", asteriskmanifest.Version)
	}
}
