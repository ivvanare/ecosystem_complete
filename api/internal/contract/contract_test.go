// Package contract keeps the API and its documentation from drifting apart.
//
// It is a test, not a hook or a checklist: `go test ./...` is the gate everybody already runs,
// so a change that adds a route, an environment variable or an error code without updating the
// contract fails there — in the same place a broken handler would.
//
// What it cannot check is whether new behaviour is *tested* beyond its error codes; that stays
// a review responsibility.
package contract

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Paths are relative to this package: api/internal/contract -> repository root.
const (
	repoRoot   = "../../.."
	httpapiDir = "../httpapi"
	apiDoc     = repoRoot + "/api/API.md"
	apiReadme  = repoRoot + "/api/README.md"
	envExample = repoRoot + "/api/.env.example"
	openapiDoc = repoRoot + "/api/openapi.yaml"
)

func read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(content)
}

// handlerSources returns every non-test Go file of the HTTP package.
func handlerSources(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Clean(httpapiDir))
	if err != nil {
		t.Fatalf("reading %s: %v", httpapiDir, err)
	}

	sources := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		sources[name] = read(t, filepath.Join(httpapiDir, name))
	}
	if len(sources) == 0 {
		t.Fatal("no handler sources found")
	}
	return sources
}

var (
	routePattern = regexp.MustCompile(`HandleFunc\("(GET|POST|PATCH|PUT|DELETE) (/[^"]*)"`)
	errorCode    = regexp.MustCompile(`(?:writeError\([^,]+,\s*[^,]+,\s*|Code:\s*)"([a-z_]+)"`)
	envVar       = regexp.MustCompile(`(?:lookup|valueOr)\(getenv, "([A-Z_]+)"`)
)

type route struct {
	method string
	path   string
}

func registeredRoutes(t *testing.T, sources map[string]string) []route {
	t.Helper()
	seen := map[route]bool{}
	for _, source := range sources {
		for _, match := range routePattern.FindAllStringSubmatch(source, -1) {
			seen[route{method: match[1], path: match[2]}] = true
		}
	}
	if len(seen) == 0 {
		t.Fatal("no routes found: the pattern in this test no longer matches the router")
	}

	routes := make([]route, 0, len(seen))
	for r := range seen {
		routes = append(routes, r)
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].path == routes[j].path {
			return routes[i].method < routes[j].method
		}
		return routes[i].path < routes[j].path
	})
	return routes
}

func emittedErrorCodes(t *testing.T, sources map[string]string) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, source := range sources {
		for _, match := range errorCode.FindAllStringSubmatch(source, -1) {
			seen[match[1]] = true
		}
	}
	if len(seen) == 0 {
		t.Fatal("no error codes found: the pattern in this test no longer matches the handlers")
	}

	codes := make([]string, 0, len(seen))
	for code := range seen {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

type openAPISpec struct {
	Paths map[string]map[string]struct {
		OperationID string `yaml:"operationId"`
	} `yaml:"paths"`
}

func TestEveryRouteIsInTheOpenAPIContract(t *testing.T) {
	routes := registeredRoutes(t, handlerSources(t))

	var spec openAPISpec
	if err := yaml.Unmarshal([]byte(read(t, openapiDoc)), &spec); err != nil {
		t.Fatalf("parsing openapi.yaml: %v", err)
	}

	documented := map[route]bool{}
	for path, methods := range spec.Paths {
		for method := range methods {
			documented[route{method: strings.ToUpper(method), path: path}] = true
		}
	}

	for _, r := range routes {
		if !documented[r] {
			t.Errorf("%s %s is registered but missing from api/openapi.yaml", r.method, r.path)
		}
	}
	for r := range documented {
		if r.method == "OPTIONS" {
			continue
		}
		found := false
		for _, registered := range routes {
			if registered == r {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s %s is in api/openapi.yaml but not registered", r.method, r.path)
		}
	}
}

func TestEveryRouteIsInTheConsumerGuide(t *testing.T) {
	guide := read(t, apiDoc)

	for _, r := range registeredRoutes(t, handlerSources(t)) {
		if !strings.Contains(guide, r.path) {
			t.Errorf("%s %s is registered but never mentioned in api/API.md", r.method, r.path)
		}
	}
}

func TestEveryErrorCodeIsInTheCatalogue(t *testing.T) {
	guide := read(t, apiDoc)

	for _, code := range emittedErrorCodes(t, handlerSources(t)) {
		if !strings.Contains(guide, "`"+code+"`") {
			t.Errorf("error code %q is emitted but absent from the catalogue in api/API.md", code)
		}
	}
}

func TestEveryErrorCodeIsExercisedByATest(t *testing.T) {
	// A proxy for "the tests were updated": every error a handler can return appears in some
	// test file. It does not prove the behaviour is covered, but it catches the common case of
	// adding a branch and forgetting its test.
	entries, err := os.ReadDir(filepath.Clean(httpapiDir))
	if err != nil {
		t.Fatalf("reading %s: %v", httpapiDir, err)
	}

	var tests strings.Builder
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), "_test.go") {
			tests.WriteString(read(t, filepath.Join(httpapiDir, entry.Name())))
		}
	}
	suite := tests.String()
	if suite == "" {
		t.Fatal("no test files found in the HTTP package")
	}

	for _, code := range emittedErrorCodes(t, handlerSources(t)) {
		if !strings.Contains(suite, code) {
			t.Errorf("error code %q is emitted but never asserted in a test", code)
		}
	}
}

func TestEveryEnvironmentVariableIsDocumented(t *testing.T) {
	config := read(t, "../config/config.go")
	readme := read(t, apiReadme)
	example := read(t, envExample)

	matches := envVar.FindAllStringSubmatch(config, -1)
	if len(matches) == 0 {
		t.Fatal("no environment variables found: the pattern in this test no longer matches config")
	}

	seen := map[string]bool{}
	for _, match := range matches {
		name := match[1]
		if seen[name] {
			continue
		}
		seen[name] = true

		if !strings.Contains(readme, "`"+name+"`") {
			t.Errorf("%s is read from the environment but missing from the api/README.md table", name)
		}
		if !strings.Contains(example, name+"=") {
			t.Errorf("%s is read from the environment but missing from api/.env.example", name)
		}
	}
}
