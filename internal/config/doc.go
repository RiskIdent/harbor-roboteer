// Package config loads and evaluates the roboteer Jsonnet config.
//
// The config file must evaluate to a function that takes the JWT claims as
// its "claims" parameter, and returns a list of permissions. Null entries are
// dropped, so conditional entries can be written as "if cond then entry"
// without an else branch:
//
//	function(claims) [
//	  { kind: 'project', namespace: 'acme-shared', access: { repository: ['pull'] } },
//	  if claims.repository == 'acme/scanner' then
//	    { kind: 'project', namespace: '*', access: { repository: ['pull'] } },
//	]
//
// Each permission has these fields:
//
//   - kind: "project" or "system".
//   - namespace: the project name, or "*" for all projects.
//     Required for "project", not allowed for "system".
//   - access: Harbor resource to list of actions, such as {repository: ["pull"]}.
//
// Permissions for the same project are merged. An empty list grants nothing,
// so the token is rejected. Imports are resolved relative to the importing file.
package config
