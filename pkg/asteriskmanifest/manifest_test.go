package asteriskmanifest

import (
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
