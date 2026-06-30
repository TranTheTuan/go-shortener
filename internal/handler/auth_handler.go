package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"

	appmw "github.com/TranTheTuan/go-shortener/internal/middleware"
	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/response"
)

// AuthHandler exposes identity endpoints. Authentication itself (register, login,
// logout, token refresh) is handled by Keycloak; this only reports the current
// user resolved from the validated access token.
type AuthHandler struct {
	users service.UserService
}

// NewAuthHandler wires an AuthHandler to the user service.
func NewAuthHandler(users service.UserService) *AuthHandler {
	return &AuthHandler{users: users}
}

// Me handles GET /auth/me. Requires a valid Keycloak access token (Keycloak
// middleware), and returns the JIT-provisioned local user.
//
// @Summary      Get the authenticated user
// @Tags         auth
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  github_com_TranTheTuan_go-shortener_internal_repository.User
// @Failure      401  {object}  response.Envelope
// @Router       /auth/me [get]
func (h *AuthHandler) Me(c echo.Context) error {
	userID, ok := appmw.UserIDFrom(c)
	if !ok {
		return response.Error(c, apperror.New(http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated"))
	}

	user, err := h.users.GetUser(c.Request().Context(), userID)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, http.StatusOK, user)
}
