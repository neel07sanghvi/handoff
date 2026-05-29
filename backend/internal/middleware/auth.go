package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	userIDKey      contextKey = "user_id"
	workspaceIDKey contextKey = "workspace_id"
)

type authClaims struct {
	WorkspaceID string `json:"workspace_id"`
	jwt.RegisteredClaims
}

func Auth(jwtSecret string) func(http.Handler) http.Handler {
	secret := []byte(jwtSecret)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, err := claimsFromRequest(r, secret)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "missing or invalid token")
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey, claims.Subject)
			ctx = context.WithValue(ctx, workspaceIDKey, claims.WorkspaceID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDKey).(string)
	return userID, ok && userID != ""
}

func WorkspaceIDFromContext(ctx context.Context) (string, bool) {
	workspaceID, ok := ctx.Value(workspaceIDKey).(string)
	return workspaceID, ok && workspaceID != ""
}

func claimsFromRequest(r *http.Request, secret []byte) (*authClaims, error) {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	tokenString, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok || strings.TrimSpace(tokenString) == "" {
		return nil, errors.New("missing bearer token")
	}

	claims := &authClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil || !token.Valid || claims.Subject == "" || claims.WorkspaceID == "" {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
