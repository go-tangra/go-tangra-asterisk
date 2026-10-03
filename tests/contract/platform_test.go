package contract_test

import (
	"context"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/httpapi"
	"github.com/go-tangra/go-tangra-asterisk/v4/pkg/asteriskmanifest"
	"testing"
)

func TestManifestRoutesRolesAndValidation(t *testing.T) {
	doc, e := asteriskmanifest.Load()
	if e != nil {
		t.Fatal(e)
	}
	if e = doc.Validate(context.Background()); e != nil {
		t.Fatal(e)
	}
	s, e := httpapi.New(httpapi.Deps{})
	if e != nil {
		t.Fatal(e)
	}
	man, e := asteriskmanifest.Manifest()
	if e != nil {
		t.Fatal(e)
	}
	if len(man.Routes) != 16 {
		t.Fatalf("missing routes: %d", len(man.Routes))
	}
	for _, route := range man.Routes {
		if !s.Patterns()[route.Method+" "+route.Path] || route.Public || route.Permission == "" {
			t.Fatalf("unsafe route %+v", route)
		}
	}
	for _, p := range asteriskmanifest.Grants["member"] {
		if p == "recordings:read" {
			t.Fatal("viewer receives recordings")
		}
	}
	for _, p := range asteriskmanifest.Grants["owner"] {
		if p == "recordings:read" {
			return
		}
	}
	t.Fatal("investigator misses recordings")
}

func TestManifestRejectsPublicOrUnknownPermission(t *testing.T) {
	for _, tc := range []struct {
		extension string
		value     any
	}{{"x-freya-public", true}, {"x-freya-permission", "undeclared:read"}} {
		doc, e := asteriskmanifest.Load()
		if e != nil {
			t.Fatal(e)
		}
		op := doc.Paths.Value("/api/asterisk/calls").Get
		op.Extensions[tc.extension] = tc.value
		if _, e = asteriskmanifest.Routes(doc); e == nil {
			t.Fatal("unsafe declaration accepted")
		}
	}
}
