package harbor

import (
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	"go.yaml.in/yaml/v3"
)

type RobotCreate struct {
	NameBase string
	Project  string
	RobotMetadata
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
	b, _ := yaml.Marshal(m) // can't error
	return string(b)
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
