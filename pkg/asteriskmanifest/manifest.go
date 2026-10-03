package asteriskmanifest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/go-tangra/go-tangra-asterisk/v4/api/openapi"
	"github.com/go-tangra/go-tangra-auth/sdk/v4/pkg/authclient"
	"github.com/go-tangra/go-tangra-portal/sdk/v4/pkg/gatewayclient"
)

// Module identity.
const (
	Module       = "asterisk"
	DisplayName  = "Asterisk"
	Version      = "1.0.0"
	RemotePrefix = "/ui"
	APIPrefix    = "/api/asterisk"
)

// OpenAPI operation extensions.
const (
	PermissionExtension = "x-freya-permission"
	PublicExtension     = "x-freya-public"
	BodyLimitExtension  = "x-freya-max-body-bytes"
	TimeoutExtension    = "x-freya-timeout-seconds"
)

// Permissions the module registers (research D16).
var Permissions = []gatewayclient.Permission{
	{Resource: "calls", Action: "read", Description: "Read calls"},
	{Resource: "stats", Action: "read", Description: "Read stats"},
	{Resource: "registration", Action: "read", Description: "Read registration"},
	{Resource: "live", Action: "read", Description: "Read live"},
	{Resource: "recordings", Action: "read", Description: "Read recordings"},
	{Resource: "dashboard", Action: "read", Description: "Read dashboard"},
}
var viewerPermissions = []string{"calls:read", "stats:read", "registration:read", "live:read", "dashboard:read"}
var Roles = []authclient.ModuleRole{
	{Slug: "viewer", DisplayName: "Asterisk viewer", Permissions: viewerPermissions},
	{Slug: "investigator", DisplayName: "Asterisk investigator", Permissions: PermissionRefs()},
}
var Grants = map[string][]string{"owner": PermissionRefs(), "admin": PermissionRefs(), "operator": viewerPermissions, "member": viewerPermissions, "auditor": viewerPermissions}

// Browser APIs use HTTP; no public gRPC methods are registered.
var Methods []gatewayclient.Method

// Abilities are the CASL rules bound to the permissions.
var Abilities = []gatewayclient.Ability{
	{Action: []string{"read"}, Subject: []string{"AsteriskCalls"}, Requires: "calls:read"},
	{Action: []string{"read"}, Subject: []string{"AsteriskStats"}, Requires: "stats:read"},
	{Action: []string{"read"}, Subject: []string{"AsteriskRegistration"}, Requires: "registration:read"},
	{Action: []string{"read"}, Subject: []string{"AsteriskLive"}, Requires: "live:read"},
	{Action: []string{"read"}, Subject: []string{"AsteriskRecordings"}, Requires: "recordings:read"},
	{Action: []string{"read"}, Subject: []string{"AsteriskDashboard"}, Requires: "dashboard:read"},
}
var Nav = []gatewayclient.NavEntry{
	{Title: "Call history", Path: "/asterisk", Icon: "mdi-phone-log", Order: 980, Requires: "calls:read"},
	{Title: "Overview", Path: "/asterisk/overview", Icon: "mdi-chart-bar", Order: 981, Requires: "stats:read"},
	{Title: "Extensions", Path: "/asterisk/extensions", Icon: "mdi-phone", Order: 982, Requires: "stats:read"},
	{Title: "Live calls", Path: "/asterisk/live", Icon: "mdi-access-point", Order: 983, Requires: "live:read"},
	{Title: "Monitoring", Path: "/asterisk/dashboards", Icon: "mdi-chart-line", Order: 984, Requires: "dashboard:read"},
}

// PermissionRefs lists "resource:action" for every declared permission.
func PermissionRefs() []string {
	out := make([]string, 0, len(Permissions))
	for _, p := range Permissions {
		out = append(out, p.Resource+":"+p.Action)
	}
	return out
}

// Routes derives the gateway routes from the OpenAPI document.
func Routes(doc *openapi3.T) ([]gatewayclient.Route, error) {
	known := map[string]bool{}
	for _, p := range PermissionRefs() {
		known[p] = true
	}
	var routes []gatewayclient.Route
	for p, item := range doc.Paths.Map() {
		if !strings.HasPrefix(p, APIPrefix+"/") {
			return nil, fmt.Errorf("route outside module API prefix")
		}
		for m, op := range item.Operations() {
			r := gatewayclient.Route{Method: strings.ToUpper(m), Path: p}
			perm, _ := op.Extensions[PermissionExtension].(string)
			public, _ := op.Extensions[PublicExtension].(bool)
			switch {
			case public:
				return nil, fmt.Errorf("asteriskmanifest: public data routes are prohibited")
			case perm == "":
				return nil, fmt.Errorf("asteriskmanifest: %s %s declares no permission", r.Method, p)
			case !known[perm]:
				return nil, fmt.Errorf("asteriskmanifest: %s %s uses undeclared permission %q", r.Method, p, perm)
			default:
				r.Permission = perm
			}
			if v, ok := op.Extensions[BodyLimitExtension]; ok {
				n, ok := v.(float64)
				if !ok || n <= 0 {
					return nil, fmt.Errorf("asteriskmanifest: %s %s has a bad body limit", r.Method, p)
				}
				r.MaxBodyBytes = uint64(n)
			}
			if v, ok := op.Extensions[TimeoutExtension]; ok {
				n, ok := v.(float64)
				if !ok || n <= 0 || n > 600 {
					return nil, fmt.Errorf("asteriskmanifest: %s %s has a bad timeout", r.Method, p)
				}
				r.Timeout = time.Duration(n) * time.Second
			}
			routes = append(routes, r)
		}
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Method+" "+routes[i].Path < routes[j].Method+" "+routes[j].Path })
	return routes, nil
}

// Load parses the embedded document.
func Load() (*openapi3.T, error) {
	return openapi3.NewLoader().LoadFromData(openapi.Asterisk)
}

// Manifest builds the gateway manifest from the embedded OpenAPI document.
func Manifest() (gatewayclient.Manifest, error) {
	doc, err := Load()
	if err != nil {
		return gatewayclient.Manifest{}, err
	}
	if err := doc.Validate(context.Background()); err != nil {
		return gatewayclient.Manifest{}, err
	}
	if err := ValidateDeclarations(); err != nil {
		return gatewayclient.Manifest{}, err
	}
	routes, err := Routes(doc)
	if err != nil {
		return gatewayclient.Manifest{}, err
	}
	return gatewayclient.Manifest{
		Module: Module, DisplayName: DisplayName, Version: Version,
		Prefixes:    []string{APIPrefix},
		Routes:      routes,
		Methods:     Methods,
		Permissions: Permissions,
		Abilities:   Abilities,
		Exposes:     []string{"./routes", "./nav"},
		Nav:         Nav,
	}, nil
}

// BuiltinRoles lists the built-in role slugs granted, in a stable order.
var BuiltinRoles = []string{"owner", "admin", "member", "auditor", "operator"}

// Registration is what the module registers with auth: every module
// permission, the module roles and the built-in role grants.
func Registration() authclient.Registration {
	reg := authclient.Registration{Module: Module, DisplayName: DisplayName, Roles: Roles, BuiltinGrants: Grants}
	for _, p := range Permissions {
		reg.Permissions = append(reg.Permissions, authclient.Permission{Resource: p.Resource, Action: p.Action, Description: p.Description})
	}
	return reg
}

// ValidateDeclarations checks grants, nav and UI abilities against the same
// permission vocabulary used by the OpenAPI operations.
func ValidateDeclarations() error {
	known := map[string]bool{}
	for _, p := range PermissionRefs() {
		known[p] = true
	}
	for _, role := range Roles {
		for _, p := range role.Permissions {
			if !known[p] {
				return fmt.Errorf("role references undeclared permission")
			}
		}
	}
	for _, perms := range Grants {
		for _, p := range perms {
			if !known[p] {
				return fmt.Errorf("grant references undeclared permission")
			}
		}
	}
	for _, n := range Nav {
		if !known[n.Requires] {
			return fmt.Errorf("navigation references undeclared permission")
		}
	}
	for _, a := range Abilities {
		if !known[a.Requires] {
			return fmt.Errorf("ability references undeclared permission")
		}
	}
	return nil
}
