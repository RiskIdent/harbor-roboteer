package config

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"go.yaml.in/yaml/v3"
)

// validatableWithContext is like [validation.Validatable], but gets our [validationContext].
type validatableWithContext interface {
	Validate(vc validationContext) error
}

var errPlaceholdersNotSupported = errors.New("placeholders like {name} are not supported here")

type validationContext struct {
	cfg               *Config
	identityNames     []map[string]bool // available names per identity, same order as identities
	availableNames    map[string]bool   // union of identityNames
	sectionsByGrantID map[string][]string
}

func newValidationContext(c *Config) validationContext {
	vc := validationContext{
		cfg:               c,
		availableNames:    make(map[string]bool),
		sectionsByGrantID: make(map[string][]string),
	}
	for id := range c.Projects.Grants {
		vc.sectionsByGrantID[id] = append(vc.sectionsByGrantID[id], "projects")
	}
	for id := range c.AllProjects.Grants {
		vc.sectionsByGrantID[id] = append(vc.sectionsByGrantID[id], "all_projects")
	}
	for id := range c.System.Grants {
		vc.sectionsByGrantID[id] = append(vc.sectionsByGrantID[id], "system")
	}
	for _, id := range c.Identities {
		names := id.AvailableNames()
		vc.identityNames = append(vc.identityNames, names)
		maps.Copy(vc.availableNames, names)
	}
	return vc
}

func (vc validationContext) validateIdentities(identities []Identity) error {
	return validation.Validate(identities,
		uniqueItems(func(id Identity) string { return flowYAML(id) }),
		validation.Each(validation.Required,
			requiredKeys[Identity](),
			noPlaceholderKeys[Identity](),
			vc.knownClaimsOnly[Identity]()),
		vc.usedIdentitiesOnly())
}

// usedIdentitiesOnly checks that each identity can be matched by some grant.
// A grant without a "when" applies to every identity.
func (vc validationContext) usedIdentitiesOnly() validation.Rule {
	return validation.By(func(v any) error {
		whens := vc.grantWhens()
		errs := validation.Errors{}
		for i, id := range v.([]Identity) {
			if !anyWhenMatches(whens, id) {
				errs[strconv.Itoa(i)] = errors.New("identity is not matched by any grant")
			}
		}
		return errs.Filter()
	})
}

// usedClaimsOnly checks that each known claim is used by some identity or grant "when".
func (vc validationContext) usedClaimsOnly() validation.Rule {
	return validation.By(func(v any) error {
		used := map[string]bool{}
		for _, id := range vc.cfg.Identities {
			maps.Copy(used, collectKeys(id))
		}
		for _, when := range vc.grantWhens() {
			maps.Copy(used, collectKeys(when))
		}
		errs := validation.Errors{}
		for i, claim := range v.([]string) {
			if !used[claim] {
				errs[strconv.Itoa(i)] = errors.New(`claim is not used by any identity or "when"`)
			}
		}
		return errs.Filter()
	})
}

// grantWhens returns the "when" of every grant in all sections.
func (vc validationContext) grantWhens() []When {
	var whens []When
	for _, grant := range vc.cfg.Projects.Grants {
		whens = append(whens, grant.When)
	}
	for _, grant := range vc.cfg.AllProjects.Grants {
		whens = append(whens, grant.When)
	}
	for _, grant := range vc.cfg.System.Grants {
		whens = append(whens, grant.When)
	}
	return whens
}

func anyWhenMatches(whens []When, id Identity) bool {
	for _, when := range whens {
		if when.MatchesIdentity(id) {
			return true
		}
	}
	return false
}

// validateGrants checks the grant IDs, and then validates every grant.
func (vc validationContext) validateGrants[M ~map[string]G, G validatableWithContext](grants M) error {
	return validation.Validate(grants,
		requiredKeys[M](),
		noPlaceholderKeys[M](),
		vc.uniqueGrantIDs[M](),
		validation.Each(validation.By(func(v any) error {
			return v.(G).Validate(vc)
		})))
}

func (vc validationContext) whenRules() []validation.Rule {
	rules := []validation.Rule{
		requiredKeys[When](),
		noPlaceholderKeys[When](),
		validation.Each(validation.Required, noPlaceholders()),
		vc.knownClaimsOnly[When](),
		vc.whenMatchesAnIdentity(),
	}
	if vc.noIdentities() {
		rules = append(rules, validation.Required.Error("required when no identities are configured"))
	}
	return rules
}

func (vc validationContext) validateRoles[V any](refs []Pattern, roles map[string]V) error {
	return validation.Validate(refs,
		uniqueItems(Pattern.String),
		vc.placeholdersFromIdentities(),
		validation.Each(validation.Required, validation.By(func(v any) error {
			// Patterns with placeholders can't be resolved until a JWT is matched to an identity,
			// so they only need to match some defined role.
			ref := v.(Pattern)
			for name := range roles {
				if ref.CanFormat(name) {
					return nil
				}
			}
			if ref.CaptureCount() > 0 {
				return fmt.Errorf("no role matches %q", ref)
			}
			return fmt.Errorf("unknown role %q", ref)
		})),
	)
}

// validateRoleDefs checks the role definitions.
// The refs are the role references of the grants that use these roles.
func validateRoleDefs[M ~map[string]P, P ~map[string][]string](roles M, refs []Pattern) error {
	return validation.Validate(roles,
		requiredKeys[M](),
		noPlaceholderKeys[M](),
		usedRolesOnly[M](refs),
		validation.Each(permissionRules[P]()...))
}

// usedRolesOnly checks that each role is matched by some role reference.
// References with placeholders count as using every role they match.
func usedRolesOnly[M ~map[string]V, V any](refs []Pattern) validation.Rule {
	return validation.By(func(v any) error {
		errs := validation.Errors{}
		for name := range v.(M) {
			if !anyPatternCanFormat(refs, name) {
				errs[name] = errors.New("role is not used by any grant")
			}
		}
		return errs.Filter()
	})
}

// projectRoleRefs returns the role references of all grants that use the project roles.
func (vc validationContext) projectRoleRefs() []Pattern {
	var refs []Pattern
	for _, grant := range vc.cfg.Projects.Grants {
		refs = append(refs, grant.Roles...)
	}
	for _, grant := range vc.cfg.AllProjects.Grants {
		refs = append(refs, grant.Roles...)
	}
	return refs
}

// systemRoleRefs returns the role references of all grants that use the system roles.
func (vc validationContext) systemRoleRefs() []Pattern {
	var refs []Pattern
	for _, grant := range vc.cfg.System.Grants {
		refs = append(refs, grant.Roles...)
	}
	return refs
}

func anyPatternCanFormat(patterns []Pattern, s string) bool {
	for _, pattern := range patterns {
		if pattern.CanFormat(s) {
			return true
		}
	}
	return false
}

// requireRolesOrPermissions checks that a grant gives something.
// Reported on the grant itself, so it must run after the field validation.
func requireRolesOrPermissions[P ~map[string][]string](roles []Pattern, permissions P) error {
	if len(roles) == 0 && len(permissions) == 0 {
		return errors.New("must have roles or permissions")
	}
	return nil
}

// permissionRules checks that resource and action names are set, and don't use the {placeholder} syntax.
func permissionRules[M ~map[string][]string]() []validation.Rule {
	return []validation.Rule{
		requiredKeys[M](),
		noPlaceholderKeys[M](),
		validation.Each(validation.Each(validation.Required, noPlaceholders())),
	}
}

// placeholdersFromOneIdentity checks that a single identity, matching the [When],
// provides every placeholder used in the grant,
// as a JWT is only ever matched against one identity.
// Without any configured identities, the [When] is the identity.
// Meant to run after the field validation, which reports unknown placeholders
// when there are identities.
func (vc validationContext) placeholdersFromOneIdentity(when When, placeholderNames map[string]bool) error {
	if len(placeholderNames) == 0 {
		return nil
	}
	for _, names := range vc.candidateNames(when) {
		if containsAllNames(names, placeholderNames) {
			return nil
		}
	}
	names := "{" + strings.Join(slices.Sorted(maps.Keys(placeholderNames)), "}, {") + "}"
	if vc.noIdentities() {
		return fmt.Errorf("with no identities configured, placeholders must be claims set in the \"when\" field: %s", names)
	}
	return fmt.Errorf("no single identity provides all placeholders: %s", names)
}

// candidateNames returns the available names of each identity that the [When] can match.
func (vc validationContext) candidateNames(when When) []map[string]bool {
	if vc.noIdentities() {
		return []map[string]bool{collectKeys(when)}
	}
	var candidates []map[string]bool
	for i, id := range vc.cfg.Identities {
		if when.MatchesIdentity(id) {
			candidates = append(candidates, vc.identityNames[i])
		}
	}
	return candidates
}

// placeholdersFromIdentities checks that all placeholders are claims or captures of the identities.
// Does nothing when no identities are configured.
func (vc validationContext) placeholdersFromIdentities() validation.Rule {
	return validation.By(func(v any) error {
		if vc.noIdentities() {
			return nil
		}
		errs := validation.Errors{}
		for i, pattern := range v.([]Pattern) {
			for name := range pattern.CaptureNames() {
				if !vc.availableNames[name] {
					errs[strconv.Itoa(i)] = fmt.Errorf("unknown placeholder {%s}: not a claim or capture of any identity", name)
					break
				}
			}
		}
		return errs.Filter()
	})
}

// whenMatchesAnIdentity checks that some identity could match the claim values in the [When].
// Does nothing when no identities are configured.
func (vc validationContext) whenMatchesAnIdentity() validation.Rule {
	return validation.By(func(v any) error {
		when := v.(When)
		if vc.noIdentities() || len(when) == 0 {
			return nil
		}
		if slices.ContainsFunc(vc.cfg.Identities, when.MatchesIdentity) {
			return nil
		}
		return errors.New("no identity can match this condition")
	})
}

// uniqueGrantIDs checks that grant IDs are only used in one config section.
func (vc validationContext) uniqueGrantIDs[M ~map[string]G, G any]() validation.Rule {
	return validation.By(func(v any) error {
		errs := validation.Errors{}
		for id := range v.(M) {
			if sections := vc.sectionsByGrantID[id]; len(sections) > 1 {
				errs[id] = fmt.Errorf("grant ID appears in multiple sections: %s", strings.Join(sections, ", "))
			}
		}
		return errs.Filter()
	})
}

// knownClaimsOnly checks that all claim names are in the known claims.
// [validation.Each] can't be used, as it doesn't pass the map key to the rule.
func (vc validationContext) knownClaimsOnly[M ~map[string]V, V any]() validation.Rule {
	return validation.By(func(v any) error {
		errs := validation.Errors{}
		for name := range v.(M) {
			if !vc.isKnownClaim(name) {
				errs[name] = errors.New("claim not listed in known_claims")
			}
		}
		return errs.Filter()
	})
}

// isKnownClaim reports whether the claim is allowed.
// All claims are allowed when known_claims isn't set.
func (vc validationContext) isKnownClaim(name string) bool {
	// nil means all claims are allowed, while an empty list allows none
	return vc.cfg.KnownClaims == nil || slices.Contains(vc.cfg.KnownClaims, name)
}

// noIdentities reports whether no identities are configured.
// Then each grant's "when" is its identity.
func (vc validationContext) noIdentities() bool {
	return len(vc.cfg.Identities) == 0
}

// noPlaceholders rejects the {placeholder} syntax, for strings that don't support patterns.
func noPlaceholders() validation.Rule {
	return validation.By(func(v any) error {
		if strings.ContainsAny(v.(string), "{}") {
			return errPlaceholdersNotSupported
		}
		return nil
	})
}

// noPlaceholderKeys is like [noPlaceholders], but for all map keys.
func noPlaceholderKeys[M ~map[string]V, V any]() validation.Rule {
	return validation.By(func(v any) error {
		errs := validation.Errors{}
		for name := range v.(M) {
			if strings.ContainsAny(name, "{}") {
				errs[name] = errPlaceholdersNotSupported
			}
		}
		return errs.Filter()
	})
}

// requiredKeys checks that no map key is an empty string.
func requiredKeys[M ~map[string]V, V any]() validation.Rule {
	return validation.By(func(v any) error {
		if _, ok := v.(M)[""]; ok {
			return errors.New("name cannot be blank")
		}
		return nil
	})
}

// uniqueItems checks that no two items have the same key.
func uniqueItems[T any](key func(T) string) validation.Rule {
	return validation.By(func(v any) error {
		errs := validation.Errors{}
		seen := make(map[string]bool)
		for i, item := range v.([]T) {
			k := key(item)
			if seen[k] {
				errs[strconv.Itoa(i)] = fmt.Errorf("duplicate entry: %s", k)
				continue
			}
			seen[k] = true
		}
		return errs.Filter()
	})
}

func collectKeys[V any](m map[string]V) map[string]bool {
	keys := make(map[string]bool, len(m))
	for k := range m {
		keys[k] = true
	}
	return keys
}

func containsAllNames(have, want map[string]bool) bool {
	for name := range want {
		if !have[name] {
			return false
		}
	}
	return true
}

// placeholderNamesIn returns the placeholder names of every pattern in the lists.
func placeholderNamesIn(patternLists ...[]Pattern) map[string]bool {
	names := map[string]bool{}
	for _, patterns := range patternLists {
		for _, pattern := range patterns {
			for name := range pattern.CaptureNames() {
				names[name] = true
			}
		}
	}
	return names
}

// flowYAML formats the value as single-line flow-style YAML, such as "{a: x}".
func flowYAML(v any) string {
	var node yaml.Node
	if err := node.Encode(v); err != nil {
		return fmt.Sprint(v)
	}
	node.Style |= yaml.FlowStyle
	b, err := yaml.Marshal(&node)
	if err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimSpace(string(b))
}

// flattenErrors turns nested [validation.Errors] into one error per leaf,
// prefixed with its path, such as "identities[0].foo: ...".
func flattenErrors(path string, err error) []error {
	switch e := err.(type) {
	case nil:
		return nil
	case validation.Errors:
		var flat []error
		for _, key := range slices.Sorted(maps.Keys(e)) {
			flat = append(flat, flattenErrors(joinPath(path, key), e[key])...)
		}
		return flat
	default:
		if path == "" {
			return []error{err}
		}
		return []error{fmt.Errorf("%s: %w", path, err)}
	}
}

// joinPath appends the key to the path, formatting digit-only keys as list indices.
func joinPath(path, key string) string {
	if key != "" && strings.Trim(key, "0123456789") == "" {
		return path + "[" + key + "]"
	}
	if path == "" {
		return key
	}
	return path + "." + key
}
