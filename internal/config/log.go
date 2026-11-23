package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// flowYAMLLogValue is shown as flow YAML by [log/slog.TextHandler],
// and as plain JSON by [log/slog.JSONHandler].
type flowYAMLLogValue struct {
	value any
}

// String implements [fmt.Stringer], used by [log/slog.TextHandler].
func (v flowYAMLLogValue) String() string {
	return flowYAML(v.value)
}

// MarshalJSON implements [json.Marshaler], used by [log/slog.JSONHandler].
func (v flowYAMLLogValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.value)
}

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
