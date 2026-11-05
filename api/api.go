// Package api embeds the OpenAPI specification of the harbor-roboteer HTTP API.
package api

import _ "embed"

// Spec is the OpenAPI spec as written in openapi.yaml, preserving its order.
//
//go:embed openapi.yaml
var Spec []byte
