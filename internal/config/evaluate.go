package config

import (
	"fmt"
	"iter"
	"maps"
	"slices"
)

type EvaluatedPermissions struct {
	ProjectPermissions    map[string]ProjectPermissions
	AllProjectPermissions ProjectPermissions
	SystemPermissions     SystemPermissions
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
	projectPermissions, err := c.evaluateProjects(claims)
	if err != nil {
		return EvaluatedPermissions{}, err
	}
	allProjectPermissions, err := c.evaluateAllProjects(claims)
	if err != nil {
		return EvaluatedPermissions{}, err
	}
	systemPermissions, err := c.evaluateSystem(claims)
	if err != nil {
		return EvaluatedPermissions{}, err
	}
	return EvaluatedPermissions{
		ProjectPermissions:    projectPermissions,
		AllProjectPermissions: allProjectPermissions,
		SystemPermissions:     systemPermissions,
	}, nil
}

func (c *Config) evaluateProjects(claims map[string]string) (map[string]ProjectPermissions, error) {
	var projectPermissions map[string]ProjectPermissions
	for _, grant := range c.Projects.Grants {
		if !grant.When.MatchesClaims(claims) {
			continue
		}
		permissions, err := permissionsFromRoles(claims, grant.Roles, c.Projects.Roles)
		if err != nil {
			return nil, err
		}
		mergePermissionsInto(&permissions, grant.Permissions)
		for _, projectPattern := range grant.Projects {
			project, err := projectPattern.Format(claims)
			if err != nil {
				return nil, fmt.Errorf("format project %q: %w", projectPattern, err)
			}
			if projectPermissions == nil {
				projectPermissions = make(map[string]ProjectPermissions)
			}
			mergeProjectPermissionsInto(projectPermissions, project, permissions)
		}
	}
	for _, permissions := range projectPermissions {
		sortPermissions(permissions)
	}
	return projectPermissions, nil
}

func (c *Config) evaluateAllProjects(claims map[string]string) (ProjectPermissions, error) {
	var permissions ProjectPermissions
	for _, grant := range c.AllProjects.Grants {
		if grant.When.MatchesClaims(claims) {
			rolePermissions, err := permissionsFromRoles(claims, grant.Roles, c.Projects.Roles)
			if err != nil {
				return nil, err
			}
			mergePermissionsInto(&permissions, rolePermissions)
			mergePermissionsInto(&permissions, grant.Permissions)
		}
	}
	sortPermissions(permissions)
	return permissions, nil
}

func (c *Config) evaluateSystem(claims map[string]string) (SystemPermissions, error) {
	var permissions SystemPermissions
	for _, grant := range c.System.Grants {
		if grant.When.MatchesClaims(claims) {
			rolePermissions, err := permissionsFromRoles(claims, grant.Roles, c.System.Roles)
			if err != nil {
				return nil, err
			}
			mergePermissionsInto(&permissions, rolePermissions)
			mergePermissionsInto(&permissions, grant.Permissions)
		}
	}
	sortPermissions(permissions)
	return permissions, nil
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
) (P, error) {
	var resolved P
	for _, rolePattern := range rolePatterns {
		roleName, err := rolePattern.Format(claims)
		if err != nil {
			return nil, fmt.Errorf("format role %q: %w", rolePattern, err)
		}
		rolePermissions, ok := roles[roleName]
		if !ok {
			return nil, fmt.Errorf("role %q not found", roleName)
		}
		mergePermissionsInto(&resolved, rolePermissions)
	}
	return resolved, nil
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
