package handler

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// FrontendHandler serves runtime configuration the embedded SPA needs. The
// values are derived from the backend's Keycloak settings so the frontend and
// backend can never drift, and changing realm/client needs no rebuild.
type FrontendHandler struct {
	authURL           string
	realm             string
	clientID          string
	paddleClientToken string
}

// NewFrontendHandler derives the public Keycloak settings from the issuer URL
// (`{authURL}/realms/{realm}`) and the client ID.
func NewFrontendHandler(issuer, clientID, paddleClientToken string) *FrontendHandler {
	authURL, realm := parseIssuer(issuer)
	return &FrontendHandler{authURL: authURL, realm: realm, clientID: clientID, paddleClientToken: paddleClientToken}
}

// appConfig is the public configuration payload for the SPA (no secrets — the
// frontend is a public OIDC client).
type appConfig struct {
	AuthURL           string `json:"authUrl"`
	Realm             string `json:"realm"`
	ClientID          string `json:"clientId"`
	PaddleClientToken string `json:"paddleClientToken,omitempty"`
}

// Config handles GET /app-config.json.
func (h *FrontendHandler) Config(c echo.Context) error {
	// Runtime config must never go stale: it drives Keycloak auth and changes on
	// redeploy. no-cache forces revalidation while still allowing a 304.
	c.Response().Header().Set("Cache-Control", "no-cache")
	return c.JSON(http.StatusOK, appConfig{
		AuthURL:           h.authURL,
		Realm:             h.realm,
		ClientID:          h.clientID,
		PaddleClientToken: h.paddleClientToken,
	})
}

// parseIssuer splits a Keycloak issuer URL "{authURL}/realms/{realm}" into its
// auth-server origin and realm name. If the marker is absent it returns the
// issuer unchanged with an empty realm.
func parseIssuer(issuer string) (authURL, realm string) {
	const marker = "/realms/"
	if i := strings.Index(issuer, marker); i >= 0 {
		return issuer[:i], strings.Trim(issuer[i+len(marker):], "/")
	}
	return issuer, ""
}
