package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"

	appmw "github.com/TranTheTuan/go-shortener/internal/middleware"
	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/response"
)

// AuthHandler exposes the authentication endpoints.
type AuthHandler struct {
	auth  service.AuthService
	users service.UserService
}

// NewAuthHandler wires an AuthHandler to its services.
func NewAuthHandler(auth service.AuthService, users service.UserService) *AuthHandler {
	return &AuthHandler{auth: auth, users: users}
}

// registerRequest is the expected body for POST /auth/register.
type registerRequest struct {
	Username string  `json:"username"`
	Email    string  `json:"email"`
	Password string  `json:"password"`
	Name     *string `json:"name,omitempty"`
}

// loginRequest is the expected body for POST /auth/login.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// refreshRequest is the expected body for POST /auth/refresh and /auth/logout.
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Register handles POST /auth/register.
//
// @Summary      Register a new user
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      registerRequest  true  "Credentials"
// @Success      201      {object}  github_com_TranTheTuan_go-shortener_internal_repository.User
// @Failure      400      {object}  response.Envelope
// @Failure      409      {object}  response.Envelope  "username or email taken"
// @Router       /auth/register [post]
func (h *AuthHandler) Register(c echo.Context) error {
	var req registerRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, apperror.BadRequest("invalid request body"))
	}

	user, err := h.auth.Register(c.Request().Context(), service.RegisterInput{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
		Name:     req.Name,
	})
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, http.StatusCreated, user)
}

// Login handles POST /auth/login.
//
// @Summary      Log in with email and password
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      loginRequest  true  "Email and password"
// @Success      200      {object}  service.TokenPair
// @Failure      400      {object}  response.Envelope
// @Failure      401      {object}  response.Envelope  "invalid credentials"
// @Router       /auth/login [post]
func (h *AuthHandler) Login(c echo.Context) error {
	var req loginRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, apperror.BadRequest("invalid request body"))
	}

	pair, err := h.auth.Login(c.Request().Context(), service.LoginInput{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, http.StatusOK, pair)
}

// Refresh handles POST /auth/refresh.
//
// @Summary      Exchange a refresh token for a new token pair
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      refreshRequest  true  "Refresh token"
// @Success      200      {object}  service.TokenPair
// @Failure      400      {object}  response.Envelope
// @Failure      401      {object}  response.Envelope  "invalid or expired token"
// @Router       /auth/refresh [post]
func (h *AuthHandler) Refresh(c echo.Context) error {
	var req refreshRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, apperror.BadRequest("invalid request body"))
	}

	pair, err := h.auth.Refresh(c.Request().Context(), req.RefreshToken)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, http.StatusOK, pair)
}

// Logout handles POST /auth/logout. It revokes the given refresh token.
//
// @Summary      Revoke a refresh token
// @Tags         auth
// @Accept       json
// @Param        request  body  refreshRequest  true  "Refresh token"
// @Success      204  "no content"
// @Failure      400  {object}  response.Envelope
// @Router       /auth/logout [post]
func (h *AuthHandler) Logout(c echo.Context) error {
	var req refreshRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, apperror.BadRequest("invalid request body"))
	}

	if err := h.auth.Logout(c.Request().Context(), req.RefreshToken); err != nil {
		return response.Error(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// Me handles GET /auth/me. Requires a valid access token (JWT middleware).
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
