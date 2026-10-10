// Command asterisk-catalogue-permissions prints the module's permissions
// (resource:action) as a JSON array for its catalogue entry (spec 035).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/go-tangra/go-tangra-asterisk/v4/pkg/asteriskmanifest"
)

func main() { os.Exit(cataloguePermissions(os.Stdout, os.Stderr)) }

func cataloguePermissions(w, errw io.Writer) int {
	m, err := asteriskmanifest.Manifest()
	if err != nil {
		fmt.Fprintln(errw, err)
		return 1
	}
	perms := make([]string, 0, len(m.Permissions))
	for _, p := range m.Permissions {
		perms = append(perms, p.Resource+":"+p.Action)
	}
	if err := json.NewEncoder(w).Encode(perms); err != nil {
		return 1
	}
	return 0
}
