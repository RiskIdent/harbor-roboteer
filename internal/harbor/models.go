package harbor

import (
	"bytes"
	"maps"
	"slices"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	"go.yaml.in/yaml/v3"
)

// Permissions is a map of "resource" -> "action". For example:
//
//	Permissions{"repository": {"pull", "push"}}
//	// => [{Resource="repository", Action="pull"}, {Resource="repository", Action="push"}]
type Permissions map[string][]string

func (p Permissions) HarborAccess() []*models.Access {
	size := 0
	for _, actions := range p {
		size += len(actions)
	}
	access := make([]*models.Access, 0, size)
	for resource, actions := range p {
		for _, action := range actions {
			access = append(access, &models.Access{
				Resource: resource,
				Action:   action,
			})
		}
	}
	return access
}

type RobotCreate struct {
	NameBase string
	RobotMetadata

	// Map of project name to set of permissions. Nil or empty means no project-level permissions.
	ProjectPermissions map[string]Permissions
	// Permissions applied to all projects. Nil or empty means no all-project permissions.
	AllProjectPermissions Permissions
	// System level permissions. Nil or empty means no system-level permissions.
	SystemPermissions Permissions
}

func (c RobotCreate) RobotPermissions() []*models.RobotPermission {
	size := len(c.ProjectPermissions)
	if len(c.AllProjectPermissions) > 0 {
		size += 1
	}
	if len(c.SystemPermissions) > 0 {
		size += 1
	}
	permissions := make([]*models.RobotPermission, 0, size)
	if len(c.SystemPermissions) > 0 {
		permissions = append(permissions, &models.RobotPermission{
			Kind:   "system",
			Access: c.SystemPermissions.HarborAccess(),
		})
	}
	if len(c.AllProjectPermissions) > 0 {
		permissions = append(permissions, &models.RobotPermission{
			Kind:      "project",
			Namespace: "*",
			Access:    c.AllProjectPermissions.HarborAccess(),
		})
	}
	for _, project := range slices.Sorted(maps.Keys(c.ProjectPermissions)) {
		permissions = append(permissions, &models.RobotPermission{
			Kind:      "project",
			Namespace: project,
			Access:    c.ProjectPermissions[project].HarborAccess(),
		})
	}
	return permissions
}

func NewRobotFromResponse(resp *models.Robot) Robot {
	return Robot{
		ID:            resp.ID,
		Name:          resp.Name,
		Secret:        resp.Secret,
		RobotMetadata: ParseRobotMetadata(resp.Description, resp.CreationTime, resp.Duration),
	}
}

type Robot struct {
	ID     int64
	Name   string
	Secret string
	RobotMetadata
}

type RobotMetadata struct {
	Description string    `yaml:"desc,omitempty"`
	EarlyExpiry time.Time `yaml:"earlyExpiry,omitempty"`
	DeleteToken string    `yaml:"deleteToken,omitempty"`
	ManagedBy   string    `yaml:"managedBy,omitempty"`
}

func (m RobotMetadata) String() string {
	if m == (RobotMetadata{}) {
		return ""
	}
	b, _ := yaml.Marshal(m) // can't error
	return string(bytes.TrimSpace(b))
}

func ParseRobotMetadata(desc string, creationTime strfmt.DateTime, duration *int64) RobotMetadata {
	var metadata RobotMetadata
	if err := yaml.Unmarshal([]byte(desc), &metadata); err != nil {
		metadata.Description = desc
	}
	if metadata.EarlyExpiry.IsZero() && duration != nil && *duration != -1 {
		metadata.EarlyExpiry = time.Time(creationTime).Add(24 * time.Hour * time.Duration(*duration))
	}
	return metadata
}
