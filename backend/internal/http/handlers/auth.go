package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neelsanghvi/handoff/backend/internal/middleware"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	db        *pgxpool.Pool
	jwtSecret []byte
}

func NewAuthHandler(db *pgxpool.Pool, jwtSecret string) *AuthHandler {
	return &AuthHandler{
		db:        db,
		jwtSecret: []byte(jwtSecret),
	}
}

type registerRequest struct {
	Name          string `json:"name"`
	Email         string `json:"email"`
	Password      string `json:"password"`
	WorkspaceName string `json:"workspace_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
}

type workspaceResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

type authResponse struct {
	Token     string            `json:"token"`
	User      userResponse      `json:"user"`
	Workspace workspaceResponse `json:"workspace"`
}

type meResponse struct {
	User      userResponse      `json:"user"`
	Workspace workspaceResponse `json:"workspace"`
}

type tokenClaims struct {
	WorkspaceID string `json:"workspace_id"`
	jwt.RegisteredClaims
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.WorkspaceName = strings.TrimSpace(req.WorkspaceName)

	if req.Name == "" || req.Email == "" || req.Password == "" || req.WorkspaceName == "" {
		writeError(w, http.StatusBadRequest, "name, email, password, and workspace_name are required")
		return
	}

	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}

	ctx := r.Context()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start transaction")
		return
	}
	defer tx.Rollback(ctx)

	var workspace workspaceResponse
	workspaceSlug := makeWorkspaceSlug(req.WorkspaceName)
	err = tx.QueryRow(ctx, `
		INSERT INTO workspaces (name, slug)
		VALUES ($1, $2)
		RETURNING id::text, name, slug, created_at
	`, req.WorkspaceName, workspaceSlug).Scan(
		&workspace.ID,
		&workspace.Name,
		&workspace.Slug,
		&workspace.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "workspace already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not create workspace")
		return
	}

	var user userResponse
	err = tx.QueryRow(ctx, `
		INSERT INTO users (workspace_id, name, email, password_hash, role)
		VALUES ($1, $2, $3, $4, 'admin')
		RETURNING id::text, workspace_id::text, name, email, role, created_at
	`, workspace.ID, req.Name, req.Email, string(passwordHash)).Scan(
		&user.ID,
		&user.WorkspaceID,
		&user.Name,
		&user.Email,
		&user.Role,
		&user.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "email already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not create user")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "could not finish registration")
		return
	}

	token, err := h.signToken(user.ID, user.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create token")
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{
		Token:     token,
		User:      user,
		Workspace: workspace,
	})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	user, workspace, passwordHash, err := h.findUserByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "invalid email or password")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not find user")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	token, err := h.signToken(user.ID, user.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create token")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{
		Token:     token,
		User:      user,
		Workspace: workspace,
	})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}

	user, workspace, err := h.findUserByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load user")
		return
	}

	writeJSON(w, http.StatusOK, meResponse{
		User:      user,
		Workspace: workspace,
	})
}

func (h *AuthHandler) findUserByEmail(ctx context.Context, email string) (userResponse, workspaceResponse, string, error) {
	var user userResponse
	var workspace workspaceResponse
	var passwordHash string

	err := h.db.QueryRow(ctx, `
		SELECT
			u.id::text,
			u.workspace_id::text,
			u.name,
			u.email,
			u.role,
			u.created_at,
			u.password_hash,
			w.id::text,
			w.name,
			w.slug,
			w.created_at
		FROM users u
		JOIN workspaces w ON w.id = u.workspace_id
		WHERE u.email = $1
	`, email).Scan(
		&user.ID,
		&user.WorkspaceID,
		&user.Name,
		&user.Email,
		&user.Role,
		&user.CreatedAt,
		&passwordHash,
		&workspace.ID,
		&workspace.Name,
		&workspace.Slug,
		&workspace.CreatedAt,
	)

	return user, workspace, passwordHash, err
}

func (h *AuthHandler) findUserByID(ctx context.Context, userID string) (userResponse, workspaceResponse, error) {
	var user userResponse
	var workspace workspaceResponse

	err := h.db.QueryRow(ctx, `
		SELECT
			u.id::text,
			u.workspace_id::text,
			u.name,
			u.email,
			u.role,
			u.created_at,
			w.id::text,
			w.name,
			w.slug,
			w.created_at
		FROM users u
		JOIN workspaces w ON w.id = u.workspace_id
		WHERE u.id = $1
	`, userID).Scan(
		&user.ID,
		&user.WorkspaceID,
		&user.Name,
		&user.Email,
		&user.Role,
		&user.CreatedAt,
		&workspace.ID,
		&workspace.Name,
		&workspace.Slug,
		&workspace.CreatedAt,
	)

	return user, workspace, err
}

func (h *AuthHandler) signToken(userID string, workspaceID string) (string, error) {
	now := time.Now()
	claims := tokenClaims{
		WorkspaceID: workspaceID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(7 * 24 * time.Hour)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(h.jwtSecret)
}

func makeWorkspaceSlug(name string) string {
	base := slugify(name)
	if base == "" {
		base = "workspace"
	}

	return base + "-" + randomHex(4)
}

func slugify(value string) string {
	var builder strings.Builder
	lastWasDash := false

	for _, r := range strings.ToLower(value) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			builder.WriteRune(r)
			lastWasDash = false
		case !lastWasDash:
			builder.WriteByte('-')
			lastWasDash = true
		}
	}

	return strings.Trim(builder.String(), "-")
}

func randomHex(byteCount int) string {
	bytes := make([]byte, byteCount)
	if _, err := rand.Read(bytes); err != nil {
		return time.Now().Format("20060102150405")
	}

	return hex.EncodeToString(bytes)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func writeJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	writeJSON(w, statusCode, map[string]string{
		"error": message,
	})
}
