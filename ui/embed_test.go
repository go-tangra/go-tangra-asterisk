//go:build ui
package ui
import("encoding/json";"io/fs";"testing")
func TestFederationRemoteEmbedded(t *testing.T){remote,ok:=Remote();if !ok{t.Fatal("built remote absent")};raw,e:=fs.ReadFile(remote,"mf-manifest.json");if e!=nil{t.Fatal(e)};var doc map[string]any;if json.Unmarshal(raw,&doc)!=nil{t.Fatal("invalid federation manifest")};if _,e=fs.Stat(remote,"remoteEntry.js");e!=nil{t.Fatal("remote entry absent")}}
