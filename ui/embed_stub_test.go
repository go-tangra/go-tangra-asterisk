//go:build !ui
package ui
import "testing"
func TestNoUIFallback(t *testing.T){if _,ok:=Remote();ok{t.Fatal("backend-only build unexpectedly advertises a remote")}}
