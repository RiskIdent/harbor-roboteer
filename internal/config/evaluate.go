package config

import (
	"cmp"
	"fmt"
	"iter"
	"maps"
	"slices"
)

type EvaluatedPermissions struct {
	ProjectPermissions    map[string]ProjectPermissions
	AllProjectPermissions ProjectPermissions
	SystemPermissions     SystemPermissions
	Grants                []AppliedGrant
}

type AppliedGrantScope string

const (
	AppliedGrantProject    AppliedGrantScope = "project"
	AppliedGrantAllProject AppliedGrantScope = "all_projects"
	AppliedGrantSystem     AppliedGrantScope = "system"
)

type AppliedGrant struct {
	// JSON and YAML tags are added for slog's JSON and Text handlers
	ID          string              `json:"id" yaml:"id"`
	Scope       AppliedGrantScope   `json:"scope" yaml:"scope"`
	Projects    []string            `json:"projects,omitempty" yaml:"projects,omitempty"`
	Roles       []string            `json:"roles,omitempty" yaml:"roles,omitempty"`
	Permissions map[string][]string `json:"permissions,omitempty" yaml:"permissions,omitempty"`
}

// String implements [fmt.Stringer].
//
// Primarily implemented so that the [log/slog.TextHandler] has a pretty representation of this value.
func (g AppliedGrant) String() string {
	return flowYAML(g)
}

func (c *Config) Identify(claims iter.Seq2[string, any]) (map[string]string, error) {
	filteredClaims, err := c.extractRequiredClaims(claims)
	if err != nil {
		return nil, err
	}

	for identity := range c.EffectiveIdentities() {
		captures, ok := identity.Match(filteredClaims)
		if !ok {
			continue
		}
		for captureName, captureValue := range captures {
			if claimValue, ok := filteredClaims[captureName]; ok && claimValue != captureValue {
				return nil, fmt.Errorf("capture %q conflicts with claim value", captureName)
			}
			filteredClaims[captureName] = captureValue
		}
		return filteredClaims, nil
	}
	return nil, fmt.Errorf("claims do not match any configured identity")
}

func (c *Config) Evaluate(claims map[string]string) (EvaluatedPermissions, error) {
	projectPermissions, projectGrants, err := c.evaluateProjects(claims)
	if err != nil {
		return EvaluatedPermissions{}, err
	}
	allProjectPermissions, allProjectGrants, err := c.evaluateAllProjects(claims)
	if err != nil {
		return EvaluatedPermissions{}, err
	}
	systemPermissions, systemGrants, err := c.evaluateSystem(claims)
	if err != nil {
		return EvaluatedPermissions{}, err
	}
	return EvaluatedPermissions{
		ProjectPermissions:    projectPermissions,
		AllProjectPermissions: allProjectPermissions,
		SystemPermissions:     systemPermissions,
		Grants:                sortedGrants(projectGrants, allProjectGrants, systemGrants),
	}, nil
}

func (c *Config) evaluateProjects(claims map[string]string) (map[string]ProjectPermissions, []AppliedGrant, error) {
	var projectPermissions map[string]ProjectPermissions
	var grants []AppliedGrant
	for grantID, grant := range c.Projects.Grants {
		if !grant.When.MatchesClaims(claims) {
			continue
		}
		permissions, roleNames, err := permissionsFromRoles(claims, grant.Roles, c.Projects.Roles)
		if err != nil {
			return nil, nil, err
		}
		mergePermissionsInto(&permissions, grant.Permissions)
		applied := AppliedGrant{
			ID:          grantID,
			Scope:       AppliedGrantProject,
			Roles:       roleNames,
			Permissions: copyPermissions(permissions),
		}
		for _, projectPattern := range grant.Projects {
			project, err := projectPattern.Format(claims)
			if err != nil {
				return nil, nil, fmt.Errorf("format project %q: %w", projectPattern, err)
			}
			if projectPermissions == nil {
				projectPermissions = make(map[string]ProjectPermissions)
			}
			mergeProjectPermissionsInto(projectPermissions, project, permissions)
			applied.Projects = append(applied.Projects, project)
		}
		slices.Sort(applied.Projects)
		grants = append(grants, applied)
	}
	for _, permissions := range projectPermissions {
		sortPermissions(permissions)
	}
	return projectPermissions, grants, nil
}

func (c *Config) evaluateAllProjects(claims map[string]string) (ProjectPermissions, []AppliedGrant, error) {
	var permissions ProjectPermissions
	var grants []AppliedGrant
	for grantID, grant := range c.AllProjects.Grants {
		if grant.When.MatchesClaims(claims) {
			rolePermissions, roleNames, err := permissionsFromRoles(claims, grant.Roles, c.Projects.Roles)
			if err != nil {
				return nil, nil, err
			}
			mergePermissionsInto(&permissions, rolePermissions)
			mergePermissionsInto(&permissions, grant.Permissions)
			grants = append(grants, AppliedGrant{
				ID:          grantID,
				Scope:       AppliedGrantAllProject,
				Roles:       roleNames,
				Permissions: mergeAndCopyPermissions(rolePermissions, grant.Permissions),
			})
		}
	}
	sortPermissions(permissions)
	return permissions, grants, nil
}

func (c *Config) evaluateSystem(claims map[string]string) (SystemPermissions, []AppliedGrant, error) {
	var permissions SystemPermissions
	var grants []AppliedGrant
	for grantID, grant := range c.System.Grants {
		if grant.When.MatchesClaims(claims) {
			rolePermissions, roleNames, err := permissionsFromRoles(claims, grant.Roles, c.System.Roles)
			if err != nil {
				return nil, nil, err
			}
			mergePermissionsInto(&permissions, rolePermissions)
			mergePermissionsInto(&permissions, grant.Permissions)
			grants = append(grants, AppliedGrant{
				ID:          grantID,
				Scope:       AppliedGrantSystem,
				Roles:       roleNames,
				Permissions: mergeAndCopyPermissions(rolePermissions, grant.Permissions),
			})
		}
	}
	sortPermissions(permissions)
	return permissions, grants, nil
}

func sortPermissions[P ~map[string][]string](permissions P) {
	for resource := range permissions {
		slices.Sort(permissions[resource])
	}
}

func permissionsFromRoles[P ~map[string][]string](
	claims map[string]string,
	rolePatterns []Pattern,
	roles map[string]P,
) (P, []string, error) {
	var resolved P
	var roleNames []string
	for _, rolePattern := range rolePatterns {
		roleName, err := rolePattern.Format(claims)
		if err != nil {
			return nil, nil, fmt.Errorf("format role %q: %w", rolePattern, err)
		}
		rolePermissions, ok := roles[roleName]
		if !ok {
			return nil, nil, fmt.Errorf("role %q not found", roleName)
		}
		roleNames = append(roleNames, roleName)
		mergePermissionsInto(&resolved, rolePermissions)
	}
	slices.Sort(roleNames)
	return resolved, roleNames, nil
}

func copyPermissions[P ~map[string][]string](permissions P) map[string][]string {
	if permissions == nil {
		return nil
	}
	clone := maps.Clone(permissions)
	for k, v := range clone {
		clone[k] = slices.Clone(v)
	}
	sortPermissions(clone)
	return clone
}

func mergeAndCopyPermissions[P ~map[string][]string](rolePermissions, directPermissions P) map[string][]string {
	permissions := copyPermissions(rolePermissions)
	mergePermissionsInto(&permissions, directPermissions)
	sortPermissions(permissions)
	return permissions
}

func sortedGrants(grantSets ...[]AppliedGrant) []AppliedGrant {
	grants := slices.Concat(grantSets...)
	slices.SortFunc(grants, func(a, b AppliedGrant) int {
		return cmp.Or(cmp.Compare(string(a.Scope), string(b.Scope)), cmp.Compare(a.ID, b.ID))
	})
	return grants
}

func mergePermissionsInto[P ~map[string][]string](destination *P, source P) {
	if *destination == nil {
		*destination = make(P)
	}
	for resource, actions := range source {
		mergedActions := Set[string]((*destination)[resource])
		if mergedActions == nil {
			mergedActions = NewSet[string](len(actions))
		}
		mergedActions.AddSeq(slices.Values(actions))
		(*destination)[resource] = mergedActions
	}
}

func mergeProjectPermissionsInto[P ~map[string][]string](projects map[string]P, project string, source P) {
	permissions := projects[project]
	mergePermissionsInto(&permissions, source)
	projects[project] = permissions
}

func (c *Config) extractRequiredClaims(claims iter.Seq2[string, any]) (map[string]string, error) {
	required := c.requiredClaimNames()
	identified := make(map[string]string, len(required))
	for name, value := range claims {
		if !required.Contains(name) {
			continue
		}
		stringValue, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("claim %q must be a string, got %T", name, value)
		}
		identified[name] = stringValue
	}
	return identified, nil
}

func (c *Config) requiredClaimNames() Set[string] {
	names := NewSet[string](len(c.KnownClaims))
	for _, identity := range c.Identities {
		names.AddSeq(maps.Keys(identity))
	}
	for _, grant := range c.Projects.Grants {
		names.AddSeq(maps.Keys(grant.When))
	}
	for _, grant := range c.AllProjects.Grants {
		names.AddSeq(maps.Keys(grant.When))
	}
	for _, grant := range c.System.Grants {
		names.AddSeq(maps.Keys(grant.When))
	}
	names.AddSeq(slices.Values(c.KnownClaims))
	return names
}
