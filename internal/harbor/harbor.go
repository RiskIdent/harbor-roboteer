package harbor

import (
	"fmt"
	"net/url"

	httptransport "github.com/go-openapi/runtime/client"
	v2client "github.com/goharbor/go-client/pkg/sdk/v2.0/client"
)

type Client struct {
	api *v2client.HarborAPI
}

func New(harborURL, username, password string) (*Client, error) {
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
