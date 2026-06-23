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

// Get handles GET /users/:id.
//
// @Summary      Get a user by ID
// @Tags         users
// @Produce      json
// @Param        id   path      int  true  "User ID"
// @Success      200  {object}  github_com_TranTheTuan_go-shortener_internal_repository.User
// @Failure      400  {object}  response.Envelope
// @Failure      404  {object}  response.Envelope
// @Router       /users/{id} [get]
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
//
// @Summary      List users
// @Tags         users
// @Produce      json
// @Success      200  {array}   github_com_TranTheTuan_go-shortener_internal_repository.User
// @Router       /users [get]
func (h *UserHandler) List(c echo.Context) error {
	users, err := h.users.ListUsers(c.Request().Context())
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, http.StatusOK, users)
}
