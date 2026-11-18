// Package config loads, watches and validates the roboteer config file.
//
// # Vocabulary
//
//   - JWT: the OIDC token a CI job sends to roboteer.
//   - Claim: a key in the JWT, such as "repository_owner".
//   - Identity: a map of claim names to [Pattern]s. A JWT is matched against
//     exactly one identity, where every claim of the identity must match.
//   - Pattern: plain text mixed with captures, such as "acme/{repo}".
//   - Capture: a "{name}" in an identity's pattern. Captures the part of the
//     claim value it matches. The name inside the braces is the capture name.
//   - Placeholder: a "{name}" in a grant's pattern, such as in "{repo}-images".
//     Filled in using the matched identity. The name must be either a claim name
//     or a capture name of that identity.
//   - Available names: an identity's claim names plus its capture names.
//     Every placeholder in a grant must be in the available names
//     of one single identity that also matches the grant's "when".
//   - Grant: a rule that gives roles and/or permissions, identified by its ID.
//     Optionally narrowed by a "when".
//   - When: literal claim values, without placeholders, that a grant additionally requires.
//     Without any configured identities, the "when" acts as the identity.
//
// # Example
//
// Given the identity {repository: "acme/{repo}"} and the JWT claim
// repository=acme/web, the capture "repo" is "web". A grant with
// projects: ["{repo}-images"] then applies to the project "web-images".
package config
