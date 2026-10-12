package asteriskmanifest

import (
	"os"
	"regexp"
	"testing"
)

// gatewayRoutePath is the route-path pattern of the portal gateway's manifest
// schema: path parameters must be lower snake_case. A route outside it makes
// the gateway refuse the whole registration (manifest_invalid).
var gatewayRoutePath = regexp.MustCompile(`^/[A-Za-z0-9._~-]+(/([A-Za-z0-9._~-]+|\{[a-z][a-z0-9_]*\}))*(/\{[a-z][a-z0-9_]*\.\.\.\})?$`)

func TestRoutePathsAcceptedByGateway(t *testing.T) {
	m, err := Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Routes) == 0 {
		t.Fatal("manifest declares no routes")
	}
	for _, r := range m.Routes {
		if !gatewayRoutePath.MatchString(r.Path) {
			t.Errorf("route %s %s is refused by the gateway manifest schema", r.Method, r.Path)
		}
	}
}

// TestNavIconsInKitSet: the shell renders navigation icons from its own
// stylesheet, which carries only the kit's icon set (@go-tangra/ui ICONS).
func TestNavIconsInKitSet(t *testing.T) {
	raw, err := os.ReadFile("../../ui/node_modules/@go-tangra/ui/dist/icons.d.ts")
	if err != nil {
		if os.Getenv("ASTERISK_REQUIRE_UI_KIT") != "" {
			t.Fatal(err)
		}
		t.Skipf("kit not installed (npm ci in ui/): %v", err)
	}
	decl := regexp.MustCompile(`ICONS: readonly \[([^\]]*)\]`).FindSubmatch(raw)
	if decl == nil {
		t.Fatal("no ICONS declaration in the kit")
	}
	kit := map[string]bool{}
	for _, m := range regexp.MustCompile(`"(mdi-[a-z0-9-]+)"`).FindAllSubmatch(decl[1], -1) {
		kit[string(m[1])] = true
	}
	for _, n := range Nav {
		if !kit[n.Icon] {
			t.Errorf("nav %s: icon %s is not in the kit set", n.Path, n.Icon)
		}
	}
}
