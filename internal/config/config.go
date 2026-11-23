package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/google/go-jsonnet"
	"github.com/google/go-jsonnet/ast"
)

// Config is a parsed Jsonnet config. It is safe for concurrent use.
type Config struct {
	path     string
	source   string
	runtimes sync.Pool // *runtime, as a [jsonnet.VM] is not safe for concurrent use
}

// Permissions are the permissions granted to one token.
// Actions are sorted and without duplicates.
type Permissions struct {
	Projects    map[string]Access `json:"projects" yaml:"projects"`
	AllProjects Access            `json:"allProjects" yaml:"allProjects"`
	System      Access            `json:"system" yaml:"system"`
}

// Access maps Harbor resources to actions, such as {"repository": ["pull", "push"]}.
type Access map[string]Set[string]

// Load reads and parses the config file.
func Load(path string) (*Config, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		// don't need to include "path" in error, as [os.ReadFile] already does that
		return nil, fmt.Errorf("load config file: %w", err)
	}
	return Parse(path, source)
}

// Parse parses the Jsonnet file content. The path is shown in errors,
// and relative imports are resolved from its directory.
func Parse(path string, source []byte) (*Config, error) {
	c := &Config{path: path, source: string(source)}
	rt, err := c.newRuntime()
	if err != nil {
		return nil, err
	}
	c.runtimes.Put(rt)
	return c, nil
}

// Evaluate calls the config function with the claims as its "claims" argument,
// and merges the returned permissions.
func (c *Config) Evaluate(claims map[string]any) (Permissions, error) {
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return Permissions{}, fmt.Errorf("encode claims: %w", err)
	}
	output, err := c.evaluate(string(claimsJSON))
	if err != nil {
		return Permissions{}, err
	}
	var entries []*permissionEntry
	decoder := json.NewDecoder(strings.NewReader(output))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entries); err != nil {
		return Permissions{}, fmt.Errorf("config must return a list of permissions: %w", err)
	}
	return newPermissions(entries)
}

// IsEmpty reports whether no permissions are granted.
func (p Permissions) IsEmpty() bool {
	return len(p.Projects) == 0 && len(p.AllProjects) == 0 && len(p.System) == 0
}

// LogValue implements [slog.LogValuer], for the audit log.
// Empty categories are logged as {} instead of null.
func (p Permissions) LogValue() slog.Value {
	p.Projects = emptyIfNil(p.Projects)
	p.AllProjects = emptyIfNil(p.AllProjects)
	p.System = emptyIfNil(p.System)
	return slog.AnyValue(flowYAMLLogValue{p})
}

func emptyIfNil[M ~map[K]V, K comparable, V any](m M) M {
	if m == nil {
		return M{}
	}
	return m
}

func (c *Config) evaluate(claimsJSON string) (string, error) {
	rt, ok := c.runtimes.Get().(*runtime)
	if !ok {
		var err error
		if rt, err = c.newRuntime(); err != nil {
			return "", err
		}
	}
	defer c.runtimes.Put(rt)
	// a top-level argument, unlike an ext var, keeps the VM's cache of evaluated imports
	rt.vm.TLACode("claims", claimsJSON)
	return rt.vm.Evaluate(rt.node)
}

// runtime is a VM together with its own parsed copy of the config.
// The VM caches evaluated imports, so they are only evaluated once per runtime.
type runtime struct {
	vm   *jsonnet.VM
	node ast.Node
}

func (c *Config) newRuntime() (*runtime, error) {
	node, err := jsonnet.SnippetToAST(c.path, c.source)
	if err != nil {
		return nil, err
	}
	vm := jsonnet.MakeVM()
	vm.Importer(&jsonnet.FileImporter{}) // resolves imports relative to the importing file
	return &runtime{vm: vm, node: node}, nil
}

type permissionEntry struct {
	Kind      string              `json:"kind"`
	Namespace string              `json:"namespace"`
	Access    map[string][]string `json:"access"`
}

func newPermissions(entries []*permissionEntry) (Permissions, error) {
	var p Permissions
	for i, entry := range entries {
		if entry == nil {
			continue // from "if cond then entry" without an else
		}
		if err := p.add(*entry); err != nil {
			return Permissions{}, fmt.Errorf("permission %d: %w", i, err)
		}
	}
	return p, nil
}

func (p *Permissions) add(entry permissionEntry) error {
	if len(entry.Access) == 0 {
		return errors.New("access must not be empty")
	}
	switch entry.Kind {
	case "system":
		if entry.Namespace != "" && entry.Namespace != "/" {
			return errors.New(`system permissions must have no namespace, or "/"`)
		}
		p.System.add(entry.Access)
	case "project":
		switch entry.Namespace {
		case "":
			return errors.New("project permissions require a namespace")
		case "*":
			p.AllProjects.add(entry.Access)
		default:
			if p.Projects == nil {
				p.Projects = make(map[string]Access)
			}
			access := p.Projects[entry.Namespace]
			access.add(entry.Access)
			p.Projects[entry.Namespace] = access
		}
	default:
		return fmt.Errorf(`unknown kind %q, must be "project" or "system"`, entry.Kind)
	}
	return nil
}

func (a *Access) add(other map[string][]string) {
	if *a == nil {
		*a = make(Access, len(other))
	}
	for resource, actions := range other {
		merged := (*a)[resource]
		merged.AddSeq(slices.Values(actions))
		slices.Sort(merged)
		(*a)[resource] = merged
	}
}
