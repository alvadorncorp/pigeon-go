package pigeon_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestProviderDependencies(t *testing.T) {
	const module = "go.alvadorn.org/pigeon"
	const root = module + "/internal/provider"
	const message = module + "/internal/message"
	const transport = root + "/internal/httptransport"
	allowed := map[string][]string{
		root:               {},
		root + "/aws":      {root, message, transport},
		root + "/resend":   {root, message, transport},
		root + "/mailgun":  {root, message, transport},
		root + "/sendgrid": {root, message, transport},
		root + "/local":    {root, message},
		transport:          {root, message},
	}
	output, err := exec.Command("go", "list", "-json", "./internal/provider/...").Output()
	if err != nil {
		t.Fatalf("list provider packages: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	seen := make(map[string]bool)
	for {
		var pkg struct {
			ImportPath string
			Imports    []string
			Module     *struct{ Path string }
		}
		if err := decoder.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("decode provider package: %v", err)
		}
		imports, ok := allowed[pkg.ImportPath]
		if !ok {
			t.Errorf("provider package %s has no dependency rule", pkg.ImportPath)
			continue
		}
		seen[pkg.ImportPath] = true
		if pkg.Module == nil || pkg.Module.Path != module {
			t.Errorf("%s must belong to module %s", pkg.ImportPath, module)
		}
		for _, dependency := range pkg.Imports {
			if dependency != module && !strings.HasPrefix(dependency, module+"/") {
				continue
			}
			if !slices.Contains(imports, dependency) {
				t.Errorf("forbidden dependency: %s -> %s", pkg.ImportPath, dependency)
			}
		}
	}
	for path := range allowed {
		if !seen[path] {
			t.Errorf("missing provider package %s", path)
		}
	}
}
