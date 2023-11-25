package main

import (
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
	"net/http"
)

type PostBcryptCompairHandler struct {
	Password       string `json:"password"`
	HashedPassword string `json:"hashed_password"`
}

type PostBcryptSumHandler struct {
	Password string `json:"password"`
}

type PostBcryptSumResult struct {
	HashedPassword string `json:"hashed_password"`
}

func bcryptCompairHandler(c echo.Context) error {
	req := new(PostBcryptCompairHandler)
	if err := c.Bind(req); err != nil {
		return err
	}

	err := bcrypt.CompareHashAndPassword([]byte(req.HashedPassword), []byte(req.Password))
	if err != nil {
		if err == bcrypt.ErrMismatchedHashAndPassword {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid username or password")
		}
		return err
	}

	return c.NoContent(200)
}

func bcryptSumHandler(c echo.Context) error {
	req := new(PostBcryptSumHandler)
	if err := c.Bind(req); err != nil {
		return err
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	res := &PostBcryptSumResult{
		HashedPassword: string(hashedPassword),
	}

	return c.JSON(200, res)
}
