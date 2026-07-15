// Package paddle wraps the PaddleHQ Go SDK, exposing only what the app needs:
// webhook signature verification and server-side API calls (portal sessions).
package paddle

import (
	"context"

	paddlesdk "github.com/PaddleHQ/paddle-go-sdk/v5"
)

// Client is the interface the app depends on for Paddle API calls.
// Wrapping the SDK behind an interface allows test doubles and decouples
// service code from the concrete SDK type.
type Client interface {
	CreatePortalSession(ctx context.Context, customerID string) (string, error)
}

// NewVerifier returns a Paddle webhook verifier for the given secret key.
// Call verifier.Verify(r) in middleware to authenticate incoming webhooks.
func NewVerifier(secret string) *paddlesdk.WebhookVerifier {
	return paddlesdk.NewWebhookVerifier(secret)
}

// NewClient returns a production Paddle API client.
func NewClient(apiKey, baseURL string) (Client, error) {
	sdk, err := paddlesdk.New(apiKey, paddlesdk.WithBaseURL(baseURL))
	if err != nil {
		return nil, err
	}
	return &paddleClient{sdk: sdk}, nil
}

type paddleClient struct {
	sdk *paddlesdk.SDK
}

// CreatePortalSession creates a Paddle Customer Portal session and returns
// the overview URL the user should be redirected to.
func (c *paddleClient) CreatePortalSession(ctx context.Context, customerID string) (string, error) {
	session, err := c.sdk.CreateCustomerPortalSession(ctx, &paddlesdk.CreateCustomerPortalSessionRequest{
		CustomerID: customerID,
	})
	if err != nil {
		return "", err
	}
	return session.URLs.General.Overview, nil
}
