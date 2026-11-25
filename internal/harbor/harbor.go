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
	createAttempts = 5
	maxNameLen     = 128
	managedByName  = "harbor-roboteer"
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
	return &Client{api: newHarborAPI(harborURL, username, password)}, nil
}

// newHarborAPI only populates the services we use, as [v2client.New] can't
// take the OpenTelemetry-instrumented transport.
func newHarborAPI(harborURL *url.URL, username, password string) *v2client.HarborAPI {
	transport := httptransport.New(harborURL.Host, v2client.DefaultBasePath, []string{harborURL.Scheme}).WithOpenTelemetry()
	auth := httptransport.BasicAuth(username, password)
	return &v2client.HarborAPI{
		Transport: transport,
		Ping:      ping.New(transport, strfmt.Default, auth),
		Robot:     robot.New(transport, strfmt.Default, auth),
	}
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
	return NewRobotFromResponse(resp.Payload), nil
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

// CreateRobot creates a project-level robot account with pull and push access to project.
func (c *Client) CreateRobot(ctx context.Context, robot RobotCreate) (Robot, error) {
	now := time.Now()
	for range createAttempts {
		request := newRobotCreateRequest(robot, now, strings.ToLower(rand.Text()[:8]))
		created, err := c.createRobot(ctx, request)
		if !errors.Is(err, ErrRobotExists) {
			return created, err
		}
	}
	return Robot{}, ErrRobotExists
}

func newRobotCreateRequest(robot RobotCreate, now time.Time, suffix string) *models.RobotCreate {
	return &models.RobotCreate{
		Name:        normalizeRobotName(robot.NameBase, suffix),
		Description: robot.RobotMetadata.String(), //nolint:staticcheck // want to be explicit that we take the String from the embedded metadata only
		Duration:    daysUntil(now, robot.Expiry),
		Level:       "system",
		Permissions: robot.RobotPermissions(),
	}
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

func (c *Client) createRobot(ctx context.Context, request *models.RobotCreate) (Robot, error) {
	resp, err := c.api.Robot.CreateRobot(ctx, robot.NewCreateRobotParams().WithRobot(request))
	if err != nil {
		return Robot{}, prettyRobotCreateError(err)
	}
	return NewRobotFromResponse(&models.Robot{
		ID:           resp.Payload.ID,
		Name:         resp.Payload.Name,
		Secret:       resp.Payload.Secret,
		CreationTime: resp.Payload.CreationTime,
		Description:  request.Description,
		Duration:     &request.Duration,
		Editable:     true,
		Level:        "project",
	}), nil
}

func prettyRobotCreateError(err error) error {
	if typed, ok := errors.AsType[*runtime.APIError](err); ok && typed.Code == http.StatusConflict {
		return ErrRobotExists
	} else if typed, ok := errors.AsType[*robot.CreateRobotBadRequest](err); ok {
		return fmt.Errorf("create robot: %w", joinErrors(typed.Payload))
	} else if typed, ok := errors.AsType[*robot.CreateRobotUnauthorized](err); ok {
		return fmt.Errorf("create robot: %w", joinErrors(typed.Payload))
	} else if typed, ok := errors.AsType[*robot.CreateRobotForbidden](err); ok {
		return fmt.Errorf("create robot: %w", joinErrors(typed.Payload))
	} else if typed, ok := errors.AsType[*robot.CreateRobotNotFound](err); ok {
		return fmt.Errorf("create robot: %w", joinErrors(typed.Payload))
	} else if typed, ok := errors.AsType[*robot.CreateRobotInternalServerError](err); ok {
		return fmt.Errorf("create robot: %w", joinErrors(typed.Payload))
	}
	return err
}

// DeleteRobot deletes the robot account with the given ID, or returns ErrRobotNotFound.
func (c *Client) DeleteRobot(ctx context.Context, id int64) error {
	if _, err := c.api.Robot.DeleteRobot(ctx, robot.NewDeleteRobotParams().WithRobotID(id)); err != nil {
		return prettyRobotDeleteError(err)
	}
	return nil
}

func prettyRobotDeleteError(err error) error {
	if typed, ok := errors.AsType[*robot.DeleteRobotBadRequest](err); ok {
		return fmt.Errorf("delete robot: %w", joinErrors(typed.Payload))
	} else if typed, ok := errors.AsType[*robot.DeleteRobotUnauthorized](err); ok {
		return fmt.Errorf("delete robot: %w", joinErrors(typed.Payload))
	} else if typed, ok := errors.AsType[*robot.DeleteRobotForbidden](err); ok {
		return fmt.Errorf("delete robot: %w", joinErrors(typed.Payload))
	} else if _, ok := errors.AsType[*robot.DeleteRobotNotFound](err); ok {
		return ErrRobotNotFound
	} else if typed, ok := errors.AsType[*robot.DeleteRobotInternalServerError](err); ok {
		return fmt.Errorf("delete robot: %w", joinErrors(typed.Payload))
	}
	return err
}

// Ping pings the harbor api to check if it is reachable.
func (c *Client) Ping(ctx context.Context) error {
	if _, err := c.api.Ping.GetPing(ctx, ping.NewGetPingParams()); err != nil {
		return fmt.Errorf("ping harbor: %w", err)
	}
	return nil
}

// CleanupRobots deletes every robot account created by harbor-roboteer whose expiry has passed.
func (c *Client) CleanupRobots(ctx context.Context, now time.Time) (int, error) {
	var expired []Robot
	for page := int64(1); ; page++ {
		resp, err := c.api.Robot.ListRobot(ctx, robot.NewListRobotParams().WithPage(&page))
		if err != nil {
			return 0, fmt.Errorf("list robots page %d: %w", page, err)
		}
		if len(resp.Payload) == 0 {
			break
		}
		for _, r := range resp.Payload {
			rob := NewRobotFromResponse(r)
			manager, _, _ := strings.Cut(rob.ManagedBy, " ")
			if manager == managedByName && !rob.Expiry.IsZero() && rob.Expiry.Before(now) {
				expired = append(expired, rob)
			}
		}
	}

	var errs []error
	deleted := 0
	for _, rob := range expired {
		if ctx.Err() != nil {
			errs = append(errs, ctx.Err())
			break
		}
		if err := c.DeleteRobot(ctx, rob.ID); err != nil && !errors.Is(err, ErrRobotNotFound) {
			errs = append(errs, fmt.Errorf("robot %q: %w", rob.Name, err))
			continue
		}
		deleted++
	}
	return deleted, errors.Join(errs...)
}

func joinErrors(payload *models.Errors) error {
	var errs []error
	for _, err := range payload.Errors {
		if err != nil {
			errs = append(errs, errors.New(err.Message))
		}
	}
	if len(errs) == 0 {
		return errors.New("Harbor returned an error without details") //nolint:staticcheck // "Harbor" is capitalized because it's a name
	}
	return errors.Join(errs...)
}
