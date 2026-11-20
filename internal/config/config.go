package config

import (
	"errors"
	"iter"
	"slices"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type Config struct {
	KnownClaims []string `yaml:"known_claims"`

	Identities []Identity `yaml:"identities"`

	Projects Projects `yaml:"projects"`

	AllProjects AllProjects `yaml:"all_projects"`

	System System `yaml:"system"`
}

// EffectiveIdentities returns configured identities, or the grant conditions
// as identities when no explicit identities are configured.
func (c *Config) EffectiveIdentities() iter.Seq[Identity] {
	if len(c.Identities) > 0 {
		return slices.Values(c.Identities)
	}
	return func(yield func(Identity) bool) {
		for _, grant := range c.Projects.Grants {
			if !yield(grant.When.AsIdentity()) {
				return
			}
		}
		for _, grant := range c.AllProjects.Grants {
			if !yield(grant.When.AsIdentity()) {
				return
			}
		}
		for _, grant := range c.System.Grants {
			if !yield(grant.When.AsIdentity()) {
				return
			}
		}
	}
}

// Validate checks the config for semantic errors.
func (c *Config) Validate() error {
	vc := newValidationContext(c)

	err := validation.Errors{
		"known_claims": validation.Validate(c.KnownClaims,
			validation.Each(validation.Required, noPlaceholders()),
			uniqueItems(func(claim string) string { return claim }),
			vc.usedClaimsOnly()),
		"identities":   vc.validateIdentities(c.Identities),
		"projects":     c.Projects.Validate(vc),
		"all_projects": c.AllProjects.Validate(vc),
		"system":       c.System.Validate(vc),
	}.Filter()
	return errors.Join(flattenErrors("", err)...)
}

type Identity map[string]Pattern

// Match reports whether all identity patterns match the claims and returns
// the captured placeholder values.
func (id Identity) Match(claims map[string]string) (map[string]string, bool) {
	var capturedClaims map[string]string
	for claimName, claimPattern := range id {
		claimValue, ok := claims[claimName]
		if !ok {
			return nil, false
		}
		claimCaptures, ok := claimPattern.Match(claimValue)
		if !ok {
			return nil, false
		}
		for captureName, captureValue := range claimCaptures {
			if existingValue, ok := capturedClaims[captureName]; ok && existingValue != captureValue {
				// if identity has e.g "sub={foo}" and "repository={foo}",
				// then require that those two captures have same value
				return nil, false
			}
			if capturedClaims == nil {
				// size may be higher than what we need, as patterns may reuse captureNames
				size := 0
				for _, claimPattern := range id {
					size += claimPattern.CaptureCount()
				}
				capturedClaims = make(map[string]string, size)
			}
			capturedClaims[captureName] = captureValue
		}
	}
	return capturedClaims, true
}

// AvailableNames returns the claim names and capture names of the identity.
func (id Identity) AvailableNames() map[string]bool {
	names := map[string]bool{}
	for claim, pattern := range id {
		names[claim] = true
		for name := range pattern.CaptureNames() {
			names[name] = true
		}
	}
	return names
}

type Projects struct {
	// Map of roboteer project roles. These can be referenced in the grants to deduplicate your config.
	Roles map[string]ProjectPermissions `yaml:"roles"`
	// Map of permission grants. The map key is the grant "ID" (shown in audit logs).
	// All matching grants add up; nothing denies.
	Grants map[string]ProjectGrant `yaml:"grants"`
}

func (p Projects) Validate(vc validationContext) error {
	return validation.Errors{
		"roles":  validateRoleDefs(p.Roles, vc.projectRoleRefs()),
		"grants": vc.validateGrants(p.Grants),
	}.Filter()
}

type ProjectPermissions map[string][]string

type ProjectGrant struct {
	When        When               `yaml:"when,omitempty"`
	Projects    []Pattern          `yaml:"projects"`
	Roles       []Pattern          `yaml:"roles"`
	Permissions ProjectPermissions `yaml:"permissions"`
}

// PlaceholderNames returns the placeholder names used in any pattern of the grant.
func (g ProjectGrant) PlaceholderNames() map[string]bool {
	return placeholderNamesIn(g.Projects, g.Roles)
}

func (g ProjectGrant) Validate(vc validationContext) error {
	err := validation.Errors{
		"when":        validation.Validate(g.When, vc.whenRules()...),
		"projects":    validation.Validate(g.Projects, validation.Required, validation.Each(validation.Required), vc.placeholdersFromIdentities()),
		"roles":       vc.validateRoles(g.Roles, vc.cfg.Projects.Roles),
		"permissions": validation.Validate(g.Permissions, permissionRules[ProjectPermissions]()...),
	}.Filter()
	if err != nil {
		return err
	}
	if err := requireRolesOrPermissions(g.Roles, g.Permissions); err != nil {
		return err
	}
	return vc.placeholdersFromOneIdentity(g.When, g.PlaceholderNames())
}

type AllProjects struct {
	// Grants on all projects, including ones created later ("Cover all projects" in Harbor).
	// Uses the project roles.
	// All matching grants add up; nothing denies.
	Grants map[string]AllProjectGrant `yaml:"grants"`
}

func (a AllProjects) Validate(vc validationContext) error {
	return validation.Errors{
		"grants": vc.validateGrants(a.Grants),
	}.Filter()
}

type AllProjectGrant struct {
	When        When               `yaml:"when,omitempty"`
	Roles       []Pattern          `yaml:"roles"`
	Permissions ProjectPermissions `yaml:"permissions"`
}

// PlaceholderNames returns the placeholder names used in any pattern of the grant.
func (g AllProjectGrant) PlaceholderNames() map[string]bool {
	return placeholderNamesIn(g.Roles)
}

func (g AllProjectGrant) Validate(vc validationContext) error {
	err := validation.Errors{
		"when":        validation.Validate(g.When, vc.whenRules()...),
		"roles":       vc.validateRoles(g.Roles, vc.cfg.Projects.Roles),
		"permissions": validation.Validate(g.Permissions, permissionRules[ProjectPermissions]()...),
	}.Filter()
	if err != nil {
		return err
	}
	if err := requireRolesOrPermissions(g.Roles, g.Permissions); err != nil {
		return err
	}
	return vc.placeholdersFromOneIdentity(g.When, g.PlaceholderNames())
}

type System struct {
	// Map of roboteer system-scope roles. These can be referenced in the grants to deduplicate your config.
	Roles map[string]SystemPermissions `yaml:"roles"`
	// Map of permission grants. The map key is the grant "ID" (shown in audit logs).
	// All matching grants add up; nothing denies.
	Grants map[string]SystemGrant `yaml:"grants"`
}

func (s System) Validate(vc validationContext) error {
	return validation.Errors{
		"roles":  validateRoleDefs(s.Roles, vc.systemRoleRefs()),
		"grants": vc.validateGrants(s.Grants),
	}.Filter()
}

type SystemPermissions map[string][]string

type SystemGrant struct {
	When        When              `yaml:"when,omitempty"`
	Roles       []Pattern         `yaml:"roles"`
	Permissions SystemPermissions `yaml:"permissions"`
}

// PlaceholderNames returns the placeholder names used in any pattern of the grant.
func (g SystemGrant) PlaceholderNames() map[string]bool {
	return placeholderNamesIn(g.Roles)
}

func (g SystemGrant) Validate(vc validationContext) error {
	err := validation.Errors{
		"when":        validation.Validate(g.When, vc.whenRules()...),
		"roles":       vc.validateRoles(g.Roles, vc.cfg.System.Roles),
		"permissions": validation.Validate(g.Permissions, permissionRules[SystemPermissions]()...),
	}.Filter()
	if err != nil {
		return err
	}
	if err := requireRolesOrPermissions(g.Roles, g.Permissions); err != nil {
		return err
	}
	return vc.placeholdersFromOneIdentity(g.When, g.PlaceholderNames())
}

type When map[string]string

func (w When) MatchesClaims(claims map[string]string) bool {
	for claimName, expectedValue := range w {
		if claims[claimName] != expectedValue {
			return false
		}
	}
	return true
}

func (w When) AsIdentity() Identity {
	identity := make(Identity, len(w))
	for claim, value := range w {
		identity[claim] = Pattern{{Text: value}}
	}
	return identity
}

// MatchesIdentity reports whether the identity has every claim in the [When],
// with patterns matching the values.
func (w When) MatchesIdentity(id Identity) bool {
	for claim, value := range w {
		pattern, ok := id[claim]
		if !ok {
			return false
		}
		if _, ok := pattern.Match(value); !ok {
			return false
		}
	}
	return true
}
