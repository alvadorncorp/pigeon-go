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
	const root = module
	const message = module + "/internal/message"
	const providerRoot = module + "/internal/provider"
	const transport = module + "/internal/httptransport"
	allowed := map[string][]string{
		module: {
			message, providerRoot, transport,
			providerRoot + "/aws", providerRoot + "/local", providerRoot + "/mailgun",
			providerRoot + "/resend", providerRoot + "/sendgrid",
		},
		message:                    {},
		module + "/examples/send":  {module},
		module + "/examples/local": {module},
		providerRoot:               {},
		providerRoot + "/aws":      {providerRoot, message, transport},
		providerRoot + "/resend":   {providerRoot, message, transport},
		providerRoot + "/mailgun":  {providerRoot, message, transport},
		providerRoot + "/sendgrid": {providerRoot, message, transport},
		providerRoot + "/local":    {providerRoot, message},
		transport:                  {providerRoot, message},
	}
	listed := []string{
		"list",
		"-json",
		".",
		"./internal/provider/...",
		"./internal/httptransport",
		"./internal/message",
		"./examples/...",
	}
	output, err := exec.CommandContext(t.Context(), "go", listed...).Output()
	if err != nil {
		t.Fatalf("list internal and example packages: %v", err)
	}
	packages := decodeProviderPackages(t, output)
	seen := make(map[string]bool)
	for _, pkg := range packages {
		imports, ok := allowed[pkg.ImportPath]
		if !ok {
			t.Errorf("package %s has no dependency rule", pkg.ImportPath)
			continue
		}
		seen[pkg.ImportPath] = true
		checkProviderPackage(t, pkg, module, imports)
	}
	for path := range allowed {
		if !seen[path] {
			t.Errorf("missing guarded package %s", path)
		}
	}
}

type providerPackage struct {
	ImportPath string
	Imports    []string
	Module     *struct{ Path string }
}

func decodeProviderPackages(t *testing.T, output []byte) []providerPackage {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(output))
	var packages []providerPackage
	for {
		var pkg providerPackage
		if err := decoder.Decode(&pkg); err == io.EOF {
			return packages
		} else if err != nil {
			t.Fatalf("decode provider package: %v", err)
		}
		packages = append(packages, pkg)
	}
}

func checkProviderPackage(t *testing.T, pkg providerPackage, module string, imports []string) {
	t.Helper()
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
