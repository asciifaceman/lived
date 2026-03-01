package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/asciifaceman/lived/pkg/config"
	"github.com/asciifaceman/lived/pkg/dal"
	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	statusSuccess   = "success"
	rolePlayer      = "player"
	statusActive    = "active"
	claimTypeAccess = "access"
	claimTypeRef    = "refresh"
)

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type authResponseData struct {
	AccessToken  string      `json:"accessToken"`
	RefreshToken string      `json:"refreshToken"`
	Account      accountData `json:"account"`
}

type accountData struct {
	ID       uint     `json:"id"`
	Username string   `json:"username"`
	Status   string   `json:"status"`
	Roles    []string `json:"roles"`
}

type meData struct {
	Account    accountData      `json:"account"`
	Characters []characterBrief `json:"characters"`
}

type characterBrief struct {
	ID        uint   `json:"id"`
	PlayerID  uint   `json:"playerId"`
	RealmID   uint   `json:"realmId"`
	Name      string `json:"name"`
	IsPrimary bool   `json:"isPrimary"`
	Status    string `json:"status"`
}

type apiResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
	Data      any    `json:"data,omitempty"`
}

type authClaims struct {
	SessionID uint     `json:"sid"`
	Username  string   `json:"username"`
	Roles     []string `json:"roles"`
	TokenType string   `json:"typ"`
	jwt.RegisteredClaims
}

type ActorContext struct {
	AccountID uint
	SessionID uint
	Username  string
	Roles     []string
}

type contextKey string

const actorContextKey contextKey = "mmo_actor"

func RegisterRoutes(group *echo.Group, database *gorm.DB, cfg config.Config) {
	if !cfg.MMOAuthEnabled {
		return
	}

	group.POST("/register", makeRegisterHandler(database, cfg))
	group.POST("/login", makeLoginHandler(database, cfg))
	group.POST("/refresh", makeRefreshHandler(database, cfg))
	group.POST("/logout", makeLogoutHandler(database, cfg), makeAuthMiddleware(database, cfg))
	group.GET("/me", makeMeHandler(database), makeAuthMiddleware(database, cfg))
}

func makeRegisterHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req registerRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid register payload")
		}

		username := strings.TrimSpace(req.Username)
		password := req.Password
		if len(username) < 3 {
			return echo.NewHTTPError(http.StatusBadRequest, "username must be at least 3 characters")
		}
		if len(password) < 8 {
			return echo.NewHTTPError(http.StatusBadRequest, "password must be at least 8 characters")
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to secure password")
		}

		account := dal.Account{Username: username, PasswordHash: string(hash), Status: statusActive}
		if err := database.WithContext(c.Request().Context()).Create(&account).Error; err != nil {
			if isUniqueConstraint(err) {
				return echo.NewHTTPError(http.StatusConflict, "username is already taken")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to create account")
		}

		if err := database.WithContext(c.Request().Context()).Create(&dal.AccountRole{AccountID: account.ID, RoleKey: rolePlayer}).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to assign account role")
		}

		roles := []string{rolePlayer}
		resp, err := issueTokensForAccount(c.Request().Context(), database, cfg, account, roles, c.RealIP(), c.Request().UserAgent())
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to issue auth tokens")
		}

		return respondSuccess(c, http.StatusCreated, "Account registered.", resp)
	}
}

func makeLoginHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req loginRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid login payload")
		}

		username := strings.TrimSpace(req.Username)
		if username == "" || req.Password == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "username and password are required")
		}

		account := dal.Account{}
		result := database.WithContext(c.Request().Context()).Where("username = ?", username).Limit(1).Find(&account)
		if result.Error != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load account")
		}
		if result.RowsAffected == 0 || account.Status != statusActive {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
		}

		if err := bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(req.Password)); err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
		}

		roles, err := loadAccountRoles(c.Request().Context(), database, account.ID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load roles")
		}

		resp, err := issueTokensForAccount(c.Request().Context(), database, cfg, account, roles, c.RealIP(), c.Request().UserAgent())
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to issue auth tokens")
		}

		return respondSuccess(c, http.StatusOK, "Login successful.", resp)
	}
}

func makeRefreshHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req refreshRequest
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid refresh payload")
		}
		if strings.TrimSpace(req.RefreshToken) == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "refreshToken is required")
		}

		claims, err := parseToken(req.RefreshToken, cfg)
		if err != nil || claims.TokenType != claimTypeRef {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid refresh token")
		}

		accountID, err := strconv.ParseUint(claims.Subject, 10, 64)
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid refresh token subject")
		}

		hash := hashToken(req.RefreshToken)
		session := dal.AccountSession{}
		res := database.WithContext(c.Request().Context()).
			Where("id = ? AND account_id = ? AND token_hash = ?", claims.SessionID, uint(accountID), hash).
			Limit(1).
			Find(&session)
		if res.Error != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load session")
		}
		if res.RowsAffected == 0 || session.RevokedAt != nil || time.Now().UTC().After(session.ExpiresAt) {
			return echo.NewHTTPError(http.StatusUnauthorized, "refresh session is no longer valid")
		}

		now := time.Now().UTC()
		if err := database.WithContext(c.Request().Context()).Model(&dal.AccountSession{}).Where("id = ?", session.ID).Updates(map[string]any{
			"revoked_at":   now,
			"last_used_at": now,
		}).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to rotate session")
		}

		account := dal.Account{}
		accRes := database.WithContext(c.Request().Context()).Where("id = ?", uint(accountID)).Limit(1).Find(&account)
		if accRes.Error != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load account")
		}
		if accRes.RowsAffected == 0 || account.Status != statusActive {
			return echo.NewHTTPError(http.StatusUnauthorized, "account is not active")
		}

		roles, err := loadAccountRoles(c.Request().Context(), database, account.ID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load roles")
		}

		resp, err := issueTokensForAccount(c.Request().Context(), database, cfg, account, roles, c.RealIP(), c.Request().UserAgent())
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to issue auth tokens")
		}

		return respondSuccess(c, http.StatusOK, "Session refreshed.", resp)
	}
}

func makeLogoutHandler(database *gorm.DB, cfg config.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		actor, ok := ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		now := time.Now().UTC()
		if err := database.WithContext(c.Request().Context()).Model(&dal.AccountSession{}).
			Where("id = ? AND account_id = ?", actor.SessionID, actor.AccountID).
			Updates(map[string]any{"revoked_at": now, "last_used_at": now}).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to revoke session")
		}

		return respondSuccess(c, http.StatusOK, "Session revoked.", nil)
	}
}

func makeMeHandler(database *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		actor, ok := ActorFromContext(c.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing actor context")
		}

		account := dal.Account{}
		res := database.WithContext(c.Request().Context()).Where("id = ?", actor.AccountID).Limit(1).Find(&account)
		if res.Error != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load account")
		}
		if res.RowsAffected == 0 {
			return echo.NewHTTPError(http.StatusUnauthorized, "account not found")
		}

		roles, err := loadAccountRoles(c.Request().Context(), database, account.ID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load roles")
		}

		characterRows := make([]dal.Character, 0)
		if err := database.WithContext(c.Request().Context()).
			Where("account_id = ?", account.ID).
			Order("is_primary DESC, id ASC").
			Find(&characterRows).Error; err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to load account characters")
		}

		characters := make([]characterBrief, 0, len(characterRows))
		for _, row := range characterRows {
			characters = append(characters, characterBrief{
				ID:        row.ID,
				PlayerID:  row.PlayerID,
				RealmID:   row.RealmID,
				Name:      row.Name,
				IsPrimary: row.IsPrimary,
				Status:    row.Status,
			})
		}

		data := meData{Account: accountData{ID: account.ID, Username: account.Username, Status: account.Status, Roles: roles}, Characters: characters}
		return respondSuccess(c, http.StatusOK, "Account loaded.", data)
	}
}

func makeAuthMiddleware(database *gorm.DB, cfg config.Config) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			authHeader := c.Request().Header.Get("Authorization")
			if !strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing bearer token")
			}

			token := strings.TrimSpace(authHeader[len("Bearer "):])
			claims, err := parseToken(token, cfg)
			if err != nil || claims.TokenType != claimTypeAccess {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid access token")
			}

			accountID, err := strconv.ParseUint(claims.Subject, 10, 64)
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid access token subject")
			}

			session := dal.AccountSession{}
			res := database.WithContext(c.Request().Context()).
				Where("id = ? AND account_id = ?", claims.SessionID, uint(accountID)).
				Limit(1).
				Find(&session)
			if res.Error != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to load auth session")
			}
			if res.RowsAffected == 0 || session.RevokedAt != nil || time.Now().UTC().After(session.ExpiresAt) {
				return echo.NewHTTPError(http.StatusUnauthorized, "session is no longer valid")
			}

			now := time.Now().UTC()
			_ = database.WithContext(c.Request().Context()).Model(&dal.AccountSession{}).Where("id = ?", session.ID).Update("last_used_at", now).Error

			ctx := context.WithValue(c.Request().Context(), actorContextKey, ActorContext{
				AccountID: uint(accountID),
				SessionID: claims.SessionID,
				Username:  claims.Username,
				Roles:     claims.Roles,
			})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}

func issueTokensForAccount(ctx context.Context, database *gorm.DB, cfg config.Config, account dal.Account, roles []string, remoteAddr, userAgent string) (authResponseData, error) {
	now := time.Now().UTC()
	session := dal.AccountSession{
		AccountID:  account.ID,
		TokenHash:  "pending",
		ExpiresAt:  now.Add(cfg.MMORefreshTokenTTL),
		UserAgent:  userAgent,
		RemoteAddr: remoteAddr,
	}
	if err := database.WithContext(ctx).Create(&session).Error; err != nil {
		return authResponseData{}, err
	}

	accessToken, err := signToken(account, session.ID, roles, claimTypeAccess, now.Add(cfg.MMOAccessTokenTTL), cfg)
	if err != nil {
		return authResponseData{}, err
	}
	refreshToken, err := signToken(account, session.ID, roles, claimTypeRef, now.Add(cfg.MMORefreshTokenTTL), cfg)
	if err != nil {
		return authResponseData{}, err
	}

	if err := database.WithContext(ctx).Model(&dal.AccountSession{}).
		Where("id = ?", session.ID).
		Update("token_hash", hashToken(refreshToken)).Error; err != nil {
		return authResponseData{}, err
	}

	return authResponseData{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Account: accountData{
			ID:       account.ID,
			Username: account.Username,
			Status:   account.Status,
			Roles:    roles,
		},
	}, nil
}

func signToken(account dal.Account, sessionID uint, roles []string, tokenType string, expiresAt time.Time, cfg config.Config) (string, error) {
	claims := authClaims{
		SessionID: sessionID,
		Username:  account.Username,
		Roles:     roles,
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    cfg.MMOJWTIssuer,
			Subject:   fmt.Sprintf("%d", account.ID),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.MMOJWTSecret))
}

func parseToken(tokenString string, cfg config.Config) (authClaims, error) {
	claims := authClaims{}
	token, err := jwt.ParseWithClaims(tokenString, &claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(cfg.MMOJWTSecret), nil
	})
	if err != nil {
		return authClaims{}, err
	}
	if !token.Valid {
		return authClaims{}, errors.New("invalid token")
	}
	if claims.Issuer != cfg.MMOJWTIssuer {
		return authClaims{}, errors.New("unexpected token issuer")
	}
	return claims, nil
}

func loadAccountRoles(ctx context.Context, database *gorm.DB, accountID uint) ([]string, error) {
	rows := make([]dal.AccountRole, 0)
	if err := database.WithContext(ctx).Where("account_id = ?", accountID).Find(&rows).Error; err != nil {
		return nil, err
	}

	roles := make([]string, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, row.RoleKey)
	}
	if len(roles) == 0 {
		roles = []string{rolePlayer}
	}
	return roles, nil
}

func hashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func ActorFromContext(ctx context.Context) (ActorContext, bool) {
	actor, ok := ctx.Value(actorContextKey).(ActorContext)
	return actor, ok
}

func RequireAuth(database *gorm.DB, cfg config.Config) echo.MiddlewareFunc {
	return makeAuthMiddleware(database, cfg)
}

func respondSuccess(c echo.Context, code int, message string, data any) error {
	requestID := c.Response().Header().Get(echo.HeaderXRequestID)
	return c.JSON(code, apiResponse{Status: statusSuccess, Message: message, RequestID: requestID, Data: data})
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}
