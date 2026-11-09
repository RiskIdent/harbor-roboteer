package server

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/RiskIdent/harbor-roboteer/api"
	"github.com/RiskIdent/harbor-roboteer/internal/server/oapi"
)

// The "version" in api/openapi.yaml is only a placeholder, marked with
// [versionMarker]. The served specs get the version of the running application
// instead, so that the docs and the spec match what's deployed.

// openAPIJSON renders the OpenAPI spec embedded in the generated code
func openAPIJSON(appVersion string) ([]byte, error) {
	spec, err := oapi.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load embedded OpenAPI spec: %w", err)
	}
	if appVersion != "" && spec.Info != nil {
		spec.Info.Version = appVersion
	}
	b, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal OpenAPI spec: %w", err)
	}
	return append(b, '\n'), nil
}

// versionMarker is the comment that marks the line in api/openapi.yaml with
// the version to replace
const versionMarker = "# (updated during runtime)"

// versionLineRe matches the line with the versionMarker, and captures what's
// before the value, e.g. "  version:"
var versionLineRe = regexp.MustCompile(`(?m)^([ \t]*version:)[ \t].*` + regexp.QuoteMeta(versionMarker) + `$`)

// openAPIYAML returns api/openapi.yaml as written, except for the version.
// Unlike openAPIJSON, it keeps the order of the source, which the docs use to
// order the operations.
func openAPIYAML(appVersion string) ([]byte, error) {
	return setSpecVersion(api.Spec, appVersion)
}

// setSpecVersion replaces the value on the line that has the versionMarker.
// There must be exactly one such line.
func setSpecVersion(spec []byte, appVersion string) ([]byte, error) {
	if appVersion == "" {
		return spec, nil
	}
	if n := len(versionLineRe.FindAll(spec, -1)); n != 1 {
		return nil, fmt.Errorf("want exactly 1 version line marked with %q in api/openapi.yaml, got %d", versionMarker, n)
	}
	// JSON strings are valid YAML, and the quotes keep e.g. "1.10" a string
	quoted, err := json.Marshal(appVersion)
	if err != nil {
		return nil, err
	}
	return versionLineRe.ReplaceAllFunc(spec, func(line []byte) []byte {
		key := versionLineRe.FindSubmatch(line)[1]
		return fmt.Appendf(nil, "%s %s %s", key, quoted, versionMarker)
	}), nil
}
