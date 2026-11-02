package harbor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-openapi/runtime"
	httptransport "github.com/go-openapi/runtime/client"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	v2client "github.com/goharbor/go-client/pkg/sdk/v2.0/client"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/robot"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

var ErrRobotExists = errors.New("robot account already exists")

type Client struct {
	api *v2client.HarborAPI
}

func New(harborURL, username, password string) (*Client, error) {
	if err := (validation.Errors{
		"url":      validation.Validate(harborURL, validation.Required),
		"username": validation.Validate(username, validation.Required),
		"password": validation.Validate(password, validation.Required),
	}).Filter(); err != nil {
		return nil, fmt.Errorf("harbor config: %w", err)
	}

	u, err := url.Parse(harborURL)
	if err != nil {
		return nil, fmt.Errorf("parse harbor url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("harbor url %q must include scheme and host, e.g. https://harbor.example.com", harborURL)
	}
	u.Path = v2client.DefaultBasePath

	return &Client{
		api: v2client.New(v2client.Config{
			URL:      u,
			AuthInfo: httptransport.BasicAuth(username, password),
		}),
	}, nil
}

// CreateRobot creates a project-level robot account with pull and push access to project.
func (c *Client) CreateRobot(ctx context.Context, name, project, description string) (*models.RobotCreated, error) {
	resp, err := c.api.Robot.CreateRobot(ctx, robot.NewCreateRobotParams().WithRobot(&models.RobotCreate{
		Name:        name,
		Description: description,
		Duration:    1,
		Level:       "project",
		Permissions: []*models.RobotPermission{{
			Kind:      "project",
			Namespace: project,
			Access: []*models.Access{
				{Resource: "repository", Action: "pull"},
				{Resource: "repository", Action: "push"},
			},
		}},
	}))
	if err != nil {
		var apiErr *runtime.APIError
		if errors.As(err, &apiErr) && apiErr.Code == http.StatusConflict {
			return nil, ErrRobotExists
		}
		return nil, fmt.Errorf("create robot: %w", err)
	}
	return resp.Payload, nil
}
