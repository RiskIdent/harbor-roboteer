package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/RiskIdent/harbor-roboteer/api"
	"github.com/RiskIdent/harbor-roboteer/internal/server/oapi"
)

// The "version" in api/openapi.yaml is only a placeholder.
// The served specs get the version of the running application instead,
// so that the docs and the spec match what's deployed.

// openAPIJSON renders the OpenAPI spec embedded in the generated code
func openAPIJSON(appVersion string) ([]byte, error) {
	swagger, err := oapi.GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("load embedded OpenAPI spec: %w", err)
	}
	if appVersion != "" && swagger.Info != nil {
		swagger.Info.Version = appVersion
	}
	b, err := json.MarshalIndent(swagger, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal OpenAPI spec: %w", err)
	}
	return append(b, '\n'), nil
}

// infoVersionRe matches the "version" line of the top-level "info" object
var infoVersionRe = regexp.MustCompile(`(?m)^  version: .*$`)

// openAPIYAML returns api/openapi.yaml as written, except for the version.
// Unlike openAPIJSON, it keeps the order of the source, which the docs use to
// order the operations.
func openAPIYAML(appVersion string) ([]byte, error) {
	if appVersion == "" {
		return api.Spec, nil
	}
	if !infoVersionRe.Match(api.Spec) {
		return nil, errors.New(`no "  version: " line in api/openapi.yaml to replace`)
	}
	// JSON strings are valid YAML, and the quotes keep e.g. "1.10" a string
	quoted, err := json.Marshal(appVersion)
	if err != nil {
		return nil, err
	}
	return infoVersionRe.ReplaceAllLiteral(api.Spec, append([]byte("  version: "), quoted...)), nil
}
