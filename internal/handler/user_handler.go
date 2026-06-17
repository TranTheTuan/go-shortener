package handler

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/response"
)

// UserHandler exposes HTTP endpoints for the user resource.
type UserHandler struct {
	users service.UserService
}

// NewUserHandler wires a UserHandler to its service.
func NewUserHandler(users service.UserService) *UserHandler {
	return &UserHandler{users: users}
}

// createUserRequest is the expected body for POST /users.
type createUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Create handles POST /users.
func (h *UserHandler) Create(c echo.Context) error {
	var req createUserRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, apperror.BadRequest("invalid request body"))
	}

	user, err := h.users.CreateUser(c.Request().Context(), service.CreateUserInput{
		Name:  req.Name,
		Email: req.Email,
	})
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, http.StatusCreated, user)
}

// Get handles GET /users/:id.
func (h *UserHandler) Get(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return response.Error(c, apperror.BadRequest("invalid user id"))
	}

	user, err := h.users.GetUser(c.Request().Context(), id)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, http.StatusOK, user)
}

// List handles GET /users.
func (h *UserHandler) List(c echo.Context) error {
	users, err := h.users.ListUsers(c.Request().Context())
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, http.StatusOK, users)
}
