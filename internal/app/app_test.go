package app

import (
	"context"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/config"
	freya "github.com/go-tangra/go-tangra/v4"
	"net"
	"testing"
)

func TestFailedBuildReleasesRuntimeListeners(t *testing.T) {
	lis, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	addr := lis.Addr().String()
	lis.Close()
	c := config.Default()
	c.Gateway.Issuer = "https://auth.example.org"
	c.TrustDomain = "test.local"
	c.Authz.Path = "unused"
	c.Admin.Addr = "127.0.0.1:0"
	c.Server.HTTPAddr = addr
	c.Server.GRPCAddr = "127.0.0.1:0"
	c.Binding.TenantID = "t"
	c.Binding.PBXID = "p"
	c.Binding.CDRDSN = "user:secret@tcp(127.0.0.1:1)/cdr"
	_, e = Build(context.Background(), c, Options{Freya: []freya.Option{freya.WithInsecureLocalDev(), freya.WithAllowAllPolicy()}})
	if e == nil {
		t.Fatal("unavailable history accepted")
	}
	lis, e = net.Listen("tcp", addr)
	if e != nil {
		t.Fatalf("failed build leaked listener: %v", e)
	}
	lis.Close()
}
