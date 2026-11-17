package harbor

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-openapi/runtime"
	httptransport "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	v2client "github.com/goharbor/go-client/pkg/sdk/v2.0/client"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/ping"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/robot"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

var (
	ErrRobotExists   = errors.New("robot account already exists")
	ErrRobotNotFound = errors.New("robot account not found")
)

const (
	createAttempts    = 5
	maxNameLen        = 128
	earlyExpiryKey    = "Early-Expiry"
	earlyExpiryFormat = time.RFC3339
)

var invalidNameChars = regexp.MustCompile(`[^a-z0-9]+`)

type Client struct {
	api *v2client.HarborAPI
}

func New(harborURL *url.URL, username, password string) (*Client, error) {
	if err := (validation.Errors{
		"url":      validation.Validate(harborURL, validation.Required),
		"username": validation.Validate(username, validation.Required),
		"password": validation.Validate(password, validation.Required),
	}).Filter(); err != nil {
		return nil, fmt.Errorf("harbor config: %w", err)
	}

	if harborURL.Scheme == "" || harborURL.Host == "" {
		return nil, fmt.Errorf("harbor url %q must include scheme and host, e.g. https://harbor.example.com", harborURL)
	}
	harborURL.Path = v2client.DefaultBasePath

	return &Client{
		api: v2client.New(v2client.Config{
			URL:      harborURL,
			AuthInfo: httptransport.BasicAuth(username, password),
		}),
	}, nil
}

// GetRobotByID returns the robot account with the given ID, or ErrRobotNotFound.
func (c *Client) GetRobotByID(ctx context.Context, id int64) (Robot, error) {
	resp, err := c.api.Robot.GetRobotByID(ctx, robot.NewGetRobotByIDParams().WithRobotID(id))
	if err != nil {
		if _, ok := errors.AsType[*robot.GetRobotByIDNotFound](err); ok {
			return Robot{}, ErrRobotNotFound
		}
		return Robot{}, fmt.Errorf("get robot %d: %w", id, err)
	}
	return newRobotFromResponse(resp.Payload), nil
}

// normalizeRobotName turns any identifier into a valid Harbor robot name.
func normalizeRobotName(base, suffix string) string {
	base = strings.Trim(invalidNameChars.ReplaceAllString(strings.ToLower(base), "-"), "-")
	base = strings.TrimRight(base[:min(len(base), maxNameLen-len(suffix)-1)], "-")
	if base == "" {
		return suffix
	}
	return base + "-" + suffix
}

func parseRobotExpiry(desc DescriptionKV, creation strfmt.DateTime, duration *int64) *time.Time {
	if early, err := time.Parse(earlyExpiryFormat, desc.KV.Get(earlyExpiryKey)); err == nil {
		return &early
	}
	if duration != nil {
		expiry := time.Time(creation).Add(24 * time.Hour * time.Duration(*duration))
		return &expiry
	}
	return nil
}

// CreateRobot creates a project-level robot account with pull and push access to project.
func (c *Client) CreateRobot(ctx context.Context, robot RobotCreate) (Robot, error) {
	robot.Description.KV = robot.Description.KV.Clone()
	if robot.Description.KV == nil {
		robot.Description.KV = http.Header{}
	}
	var duration int64
	if robot.Expiry == nil {
		robot.Description.KV.Del(earlyExpiryKey)
		duration = -1 // means "never expire"
	} else {
		robot.Description.KV.Set(earlyExpiryKey, robot.Expiry.Format(earlyExpiryFormat))
		duration = daysUntil(time.Now(), *robot.Expiry)
	}
	for range createAttempts {
		name := normalizeRobotName(robot.NameBase, strings.ToLower(rand.Text()[:8]))
		created, err := c.createRobot(ctx, name, robot.Project, robot.Description.String(), duration)
		if !errors.Is(err, ErrRobotExists) {
			return created, err
		}
	}
	return Robot{}, ErrRobotExists
}

func daysUntil(now, date time.Time) int64 {
	const day = 24 * time.Hour
	remaining := date.Sub(now)
	duration := int64(remaining / day)
	if remaining%day > 0 {
		duration++ // to make it "round up"
	}
	return max(1, duration) // Harbor robot duration must be at least 1
}

func (c *Client) createRobot(ctx context.Context, name, project, description string, duration int64) (Robot, error) {
	resp, err := c.api.Robot.CreateRobot(ctx, robot.NewCreateRobotParams().WithRobot(&models.RobotCreate{
		Name:        name,
		Description: description,
		Duration:    duration,
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
			return Robot{}, ErrRobotExists
		}
		return Robot{}, fmt.Errorf("create robot: %w", err)
	}
	return newRobotFromResponse(&models.Robot{
		ID:           resp.Payload.ID,
		Name:         resp.Payload.Name,
		Secret:       resp.Payload.Secret,
		CreationTime: resp.Payload.CreationTime,
		Description:  description,
		Duration:     &duration,
		Editable:     true,
		Level:        "project",
	}), nil
}

func newRobotFromResponse(resp *models.Robot) Robot {
	desc := ParseDescriptionKV(resp.Description)
	expiry := parseRobotExpiry(desc, resp.CreationTime, resp.Duration)
	desc.KV.Del(earlyExpiryKey) // hide internal key from result
	return Robot{
		ID:          resp.ID,
		Name:        resp.Name,
		Secret:      resp.Secret,
		Expiry:      expiry,
		Description: desc,
	}
}

// DeleteRobot deletes the robot account with the given ID, or returns ErrRobotNotFound.
func (c *Client) DeleteRobot(ctx context.Context, id int64) error {
	if _, err := c.api.Robot.DeleteRobot(ctx, robot.NewDeleteRobotParams().WithRobotID(id)); err != nil {
		if _, ok := errors.AsType[*robot.DeleteRobotNotFound](err); ok {
			return ErrRobotNotFound
		}
		return fmt.Errorf("delete robot %d: %w", id, err)
	}
	return nil
}

// Ping pings the harbor api to check if it is reachable.
func (c *Client) Ping(ctx context.Context) error {
	if _, err := c.api.Ping.GetPing(ctx, ping.NewGetPingParams()); err != nil {
		return fmt.Errorf("ping harbor: %w", err)
	}
	return nil
}
