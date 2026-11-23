// Example config file.
//
// Must be a function taking the JWT claims, returning a list of permissions.
// Null entries are dropped, so "if ... then ..." works without an "else".
// Returning an empty list rejects the token.
//
// Each permission is an object of:
//   kind:      'project' or 'system'
//   namespace: project name, or '*' for all projects. Only for kind 'project'.
//   access:    Harbor resource to list of actions, e.g. { repository: ['pull'] }

// Helpers to build the permission objects.
local lib = {
  // Permissions on one Harbor project.
  project(name, access):: { kind: 'project', namespace: name, access: access },
  // Permissions on all projects, including ones created later ("Cover all projects" in Harbor).
  allProjects(access):: self.project('*', access),
  // System-level permissions.
  system(access):: { kind: 'system', access: access },
};

// Reusable sets of access.
local roles = {
  pull: { repository+: ['list', 'read', 'pull'], artifact+: ['list', 'read'] },
  push: self.pull { repository+: ['push'], artifact+: ['create'] },
  projectCreator: { project+: ['create'], label+: ['create', 'update'] },
};

function(claims)
  local owner = std.get(claims, 'repository_owner');
  local repository = std.get(claims, 'repository', '');
  local environment = std.get(claims, 'environment');
  local harborProject = std.asciiLower(std.strReplace(repository, '/', '-'));

  if owner == 'acme' then [
    lib.project(harborProject, roles.pull),
    if environment == 'harbor' then lib.project(harborProject, roles.push),
    if repository == 'acme/deploy-tools' then lib.project('acme-shared', roles.pull { accessory: ['list'] }),
    if repository == 'acme/image-scanner' then lib.allProjects(roles.pull),
    if repository == 'acme/harbor-bootstrap' then lib.system(roles.projectCreator),
  ]
