package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/sessions"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
)

const (
	defaultSessionIDKey      = "SESSIONID"
	defaultSessionExpiresKey = "EXPIRES"
	defaultUserIDKey         = "USERID"
	defaultUsernameKey       = "USERNAME"
	bcryptDefaultCost        = bcrypt.MinCost
)

var fallbackImage = "../img/NoImage.jpg"

type UserModel struct {
	ID             int64   `db:"id"`
	Name           string  `db:"name"`
	DisplayName    string  `db:"display_name"`
	Description    string  `db:"description"`
	HashedPassword string  `db:"password"`
	IconHash       *string `db:"icon_hash"`
}

type User struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
	Theme       Theme  `json:"theme,omitempty"`
	IconHash    string `json:"icon_hash,omitempty"`
}

type Theme struct {
	ID       int64 `json:"id"`
	DarkMode bool  `json:"dark_mode"`
}

type ThemeModel struct {
	ID       int64 `db:"id"`
	UserID   int64 `db:"user_id"`
	DarkMode bool  `db:"dark_mode"`
}

type PostUserRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	// Password is non-hashed password.
	Password string               `json:"password"`
	Theme    PostUserRequestTheme `json:"theme"`
}

type PostUserRequestTheme struct {
	DarkMode bool `json:"dark_mode"`
}

type LoginRequest struct {
	Username string `json:"username"`
	// Password is non-hashed password.
	Password string `json:"password"`
}

type PostIconRequest struct {
	Image []byte `json:"image"`
}

type PostIconResponse struct {
	ID int64 `json:"id"`
}

func getIconHandler(c echo.Context) error {
	ctx := c.Request().Context()

	username := c.Param("username")

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	var user UserModel
	if err := tx.GetContext(ctx, &user, "SELECT * FROM users WHERE name = ?", username); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "not found user that has the given username")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user: "+err.Error())
	}

	headerIconHash := c.Request().Header.Get("If-None-Match")
	if user.IconHash != nil && fmt.Sprintf(`"%s"`, *user.IconHash) == headerIconHash {
		return c.NoContent(http.StatusNotModified)
	}

	// iconがディレクトリに存在するか確認
	if _, err := os.Stat(fmt.Sprintf("%s/%d", iconDir, user.ID)); err != nil {
		if os.IsNotExist(err) {
			return c.File(fallbackImage)
		} else {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user icon: "+err.Error())
		}
	}
	// 画像を返す
	// Content-Type: image/jpeg を設定する
	c.Response().Header().Set("Content-Type", "image/jpeg")
	return c.File(fmt.Sprintf("%s/%d", iconDir, user.ID))
}

func postIconHandler(c echo.Context) error {
	ctx := c.Request().Context()

	if err := verifyUserSession(c); err != nil {
		// echo.NewHTTPErrorが返っているのでそのまま出力
		return err
	}

	// error already checked
	sess, _ := session.Get(defaultSessionIDKey, c)
	// existence already checked
	userID := sess.Values[defaultUserIDKey].(int64)

	var req *PostIconRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to decode the request body as json")
	}

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	rs, err := tx.ExecContext(ctx, "INSERT INTO icons (user_id) VALUES (?)", userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to insert icon: "+err.Error())
	}

	// 画像をファイルに書き出す
	imageFilePath := fmt.Sprintf("%s/%d", iconDir, userID)
	err = os.WriteFile(imageFilePath, req.Image, 0644)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to write image file: "+err.Error())
	}

	iconHash := sha256.Sum256(req.Image)
	_, err = tx.ExecContext(ctx, "UPDATE users SET icon_hash = ? WHERE id = ?", fmt.Sprintf("%x", iconHash), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update icon hash: "+err.Error())
	}

	iconID, err := rs.LastInsertId()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get last inserted icon id: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit: "+err.Error())
	}

	return c.JSON(http.StatusCreated, &PostIconResponse{
		ID: iconID,
	})
}

func getMeHandler(c echo.Context) error {
	ctx := c.Request().Context()

	if err := verifyUserSession(c); err != nil {
		// echo.NewHTTPErrorが返っているのでそのまま出力
		return err
	}

	// error already checked
	sess, _ := session.Get(defaultSessionIDKey, c)
	// existence already checked
	userID := sess.Values[defaultUserIDKey].(int64)

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	user, err := getUserResponse(ctx, tx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "not found user that has the userid in session")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fill user: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit: "+err.Error())
	}

	return c.JSON(http.StatusOK, user)
}

// ユーザ登録API
// POST /api/register
func registerHandler(c echo.Context) error {
	ctx := c.Request().Context()
	defer c.Request().Body.Close()

	req := PostUserRequest{}
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to decode the request body as json")
	}

	if req.Name == "pipe" {
		return echo.NewHTTPError(http.StatusBadRequest, "the username 'pipe' is reserved")
	}

	// BcryptをAPIに投げる
	api := fmt.Sprintf("%s/sum", bcryptAPI)
	breq := PostBcryptSumHandler{
		Password: req.Password,
	}
	breqJson, err := json.Marshal(breq)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to marshal json: "+err.Error())
	}

	// apiに投げる
	bres, err := http.Post(api, "application/json", bytes.NewBuffer(breqJson))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to post to bcrypt api: "+err.Error())
	}
	bresBody, err := io.ReadAll(bres.Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to read response body: "+err.Error())
	}

	if bres.StatusCode != http.StatusOK {
		bResStr := string(bresBody)
		return echo.NewHTTPError(bres.StatusCode, bResStr)
	}

	var bresJson PostBcryptSumResult
	if err := json.Unmarshal(bresBody, &bresJson); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to unmarshal json: "+err.Error())
	}

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	userModel := UserModel{
		Name:           req.Name,
		DisplayName:    req.DisplayName,
		Description:    req.Description,
		HashedPassword: bresJson.HashedPassword,
	}

	result, err := tx.NamedExecContext(ctx, "INSERT INTO users (name, display_name, description, password) VALUES(:name, :display_name, :description, :password)", userModel)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to insert user: "+err.Error())
	}

	userID, err := result.LastInsertId()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get last inserted user id: "+err.Error())
	}

	userModel.ID = userID

	themeModel := ThemeModel{
		UserID:   userID,
		DarkMode: req.Theme.DarkMode,
	}
	if _, err := tx.NamedExecContext(ctx, "INSERT INTO themes (user_id, dark_mode) VALUES(:user_id, :dark_mode)", themeModel); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to insert user theme: "+err.Error())
	}

	// if out, err := exec.Command("pdnsutil", "add-record", "u.isucon.dev", req.Name, "A", "0", powerDNSSubdomainAddress).CombinedOutput(); err != nil {
	// 	return echo.NewHTTPError(http.StatusInternalServerError, string(out)+": "+err.Error())
	// }
	dnsRecordMap.Store(req.Name, struct{}{})

	user, err := fillUserResponse(ctx, tx, userModel)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fill user: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit: "+err.Error())
	}

	return c.JSON(http.StatusCreated, user)
}

// ユーザログインAPI
// POST /api/login
func loginHandler(c echo.Context) error {
	ctx := c.Request().Context()
	defer c.Request().Body.Close()

	req := LoginRequest{}
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to decode the request body as json")
	}

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	userModel := UserModel{}
	// usernameはUNIQUEなので、whereで一意に特定できる
	err = tx.GetContext(ctx, &userModel, "SELECT * FROM users WHERE name = ?", req.Username)
	if errors.Is(err, sql.ErrNoRows) {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid username or password")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit: "+err.Error())
	}

	// BcryptをAPIに投げる
	api := fmt.Sprintf("%s/compair", bcryptAPI)
	breq := PostBcryptCompairHandler{
		Password:       req.Password,
		HashedPassword: userModel.HashedPassword,
	}
	breqJson, err := json.Marshal(breq)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to marshal json: "+err.Error())
	}

	// apiに投げる
	bres, err := http.Post(api, "application/json", bytes.NewBuffer(breqJson))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to post to bcrypt api: "+err.Error())
	}
	bresBody, err := io.ReadAll(bres.Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to read response body: "+err.Error())
	}

	if bres.StatusCode != http.StatusOK {
		bResStr := string(bresBody)
		return echo.NewHTTPError(bres.StatusCode, bResStr)
	}

	sessionEndAt := time.Now().Add(1 * time.Hour)

	sessionID := uuid.NewString()

	sess, err := session.Get(defaultSessionIDKey, c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "failed to get session")
	}

	sess.Options = &sessions.Options{
		Domain: "u.isucon.dev",
		MaxAge: int(60000),
		Path:   "/",
	}
	sess.Values[defaultSessionIDKey] = sessionID
	sess.Values[defaultUserIDKey] = userModel.ID
	sess.Values[defaultUsernameKey] = userModel.Name
	sess.Values[defaultSessionExpiresKey] = sessionEndAt.Unix()

	if err := sess.Save(c.Request(), c.Response()); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save session: "+err.Error())
	}

	return c.NoContent(http.StatusOK)
}

// ユーザ詳細API
// GET /api/user/:username
func getUserHandler(c echo.Context) error {
	ctx := c.Request().Context()
	if err := verifyUserSession(c); err != nil {
		// echo.NewHTTPErrorが返っているのでそのまま出力
		return err
	}

	username := c.Param("username")

	tx, err := dbConn.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to begin transaction: "+err.Error())
	}
	defer tx.Rollback()

	userModel := UserModel{}
	if err := tx.GetContext(ctx, &userModel, "SELECT id FROM users WHERE name = ?", username); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "not found user that has the given username")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user: "+err.Error())
	}

	user, err := getUserResponse(ctx, tx, userModel.ID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fill user: "+err.Error())
	}

	if err := tx.Commit(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to commit: "+err.Error())
	}

	return c.JSON(http.StatusOK, user)
}

func verifyUserSession(c echo.Context) error {
	sess, err := session.Get(defaultSessionIDKey, c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "failed to get session")
	}

	sessionExpires, ok := sess.Values[defaultSessionExpiresKey]
	if !ok {
		return echo.NewHTTPError(http.StatusForbidden, "failed to get EXPIRES value from session")
	}

	_, ok = sess.Values[defaultUserIDKey].(int64)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "failed to get USERID value from session")
	}

	now := time.Now()
	if now.Unix() > sessionExpires.(int64) {
		return echo.NewHTTPError(http.StatusUnauthorized, "session has expired")
	}

	return nil
}

type cachedUser struct {
	user      User
	fetchedAt time.Time
}

var userCache Map[int64, cachedUser]

func getUserResponse(ctx context.Context, tx *sqlx.Tx, id int64) (User, error) {
	fetched, found := userCache.Load(id)

	now := time.Now()
	if found && fetched.fetchedAt.Add(1*time.Second+300*time.Millisecond).After(now) {
		return fetched.user, nil
	}

	model := UserModel{}
	if err := tx.GetContext(ctx, &model, "SELECT * FROM users WHERE id = ?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, sql.ErrNoRows
		}

		return User{}, err
	}

	user, err := fillUserResponse(ctx, tx, model)

	if err != nil {
		return User{}, err
	}

	fetched.fetchedAt = now
	fetched.user = user
	userCache.Store(id, fetched)

	return user, nil
}

func fillUserResponse(ctx context.Context, tx *sqlx.Tx, userModel UserModel) (User, error) {
	themeModel := ThemeModel{}
	if err := tx.GetContext(ctx, &themeModel, "SELECT * FROM themes WHERE user_id = ?", userModel.ID); err != nil {
		return User{}, err
	}

	iconHash := userModel.IconHash
	if iconHash == nil {
		var image []byte
		if _, err := os.Stat(fmt.Sprintf("%s/%d", iconDir, userModel.ID)); err != nil {
			if os.IsNotExist(err) {
				image, err = os.ReadFile(fallbackImage)
			} else {
				return User{}, err
			}
		} else {
			image, err = os.ReadFile(fmt.Sprintf("%s/%d", iconDir, userModel.ID))
			if err != nil {
				return User{}, err
			}
		}

		iconHashStr := fmt.Sprintf("%x", sha256.Sum256(image))
		iconHash = &iconHashStr
	}

	user := User{
		ID:          userModel.ID,
		Name:        userModel.Name,
		DisplayName: userModel.DisplayName,
		Description: userModel.Description,
		Theme: Theme{
			ID:       themeModel.ID,
			DarkMode: themeModel.DarkMode,
		},
		IconHash: *iconHash,
	}

	return user, nil
}
