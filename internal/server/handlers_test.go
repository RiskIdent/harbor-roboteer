package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	"github.com/lestrrat-go/jwx/v4/jwa"
	"github.com/lestrrat-go/jwx/v4/jwt"
)

type testHarborClient struct {
	createRobotCalls int
}

func (c *testHarborClient) CreateRobot(context.Context, string, string, string) (*models.RobotCreated, error) {
	c.createRobotCalls++
	return nil, nil
}

func (*testHarborClient) GetRobotByID(context.Context, int64) (*models.Robot, error) {
	return nil, nil
}

func (*testHarborClient) DeleteRobot(context.Context, int64) error {
	return nil
}

func (*testHarborClient) Ping(context.Context) error {
	return nil
}

func TestPostTokenRejectsOversizedTrailingWhitespace(t *testing.T) {
	const key = "test signing key"
	token, err := jwt.NewBuilder().Subject("subject").Build()
	if err != nil {
		t.Fatal(err)
	}
	signedToken, err := jwt.Sign(token, jwt.WithKey(jwa.HS256(), []byte(key)))
	if err != nil {
		t.Fatal(err)
	}

	hc := &testHarborClient{}
	s := &Server{
		log:     slog.New(slog.DiscardHandler),
		harbor:  hc,
		jwtOpts: []jwt.ParseOption{jwt.WithKey(jwa.HS256(), []byte(key))},
	}
	body := `{"project":"test"}` + strings.Repeat(" ", maxBodyBytes)
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+string(signedToken))
	rec := httptest.NewRecorder()

	s.postToken(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if hc.createRobotCalls != 0 {
		t.Fatalf("CreateRobot called %d times, want 0", hc.createRobotCalls)
	}
}
