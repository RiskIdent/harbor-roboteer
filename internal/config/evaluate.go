package config

import (
	"fmt"
	"iter"
)

type EvaluatedPermissions struct {
	// TODO: add fields
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
	panic("not implemented")
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
		for name := range identity {
			names.Add(name)
		}
	}
	for _, grant := range c.Projects.Grants {
		for name := range grant.When {
			names.Add(name)
		}
	}
	for _, grant := range c.AllProjects.Grants {
		for name := range grant.When {
			names.Add(name)
		}
	}
	for _, grant := range c.System.Grants {
		for name := range grant.When {
			names.Add(name)
		}
	}
	for _, name := range c.KnownClaims {
		names.Add(name)
	}
	return names
}
