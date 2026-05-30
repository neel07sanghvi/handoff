package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neelsanghvi/handoff/backend/internal/middleware"
)

type SessionHandler struct {
	db *pgxpool.Pool
}

func NewSessionHandler(db *pgxpool.Pool) *SessionHandler {
	return &SessionHandler{db: db}
}

type createSessionRequest struct {
	Title string  `json:"title"`
	Goal  *string `json:"goal"`
}

type updateSessionRequest struct {
	Status  *string `json:"status"`
	Summary *string `json:"summary"`
}

type sessionResponse struct {
	ID          string     `json:"id"`
	TicketID    string     `json:"ticket_id"`
	UserID      string     `json:"user_id"`
	Title       string     `json:"title"`
	Goal        *string    `json:"goal"`
	Status      string     `json:"status"`
	Summary     *string    `json:"summary"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

func (h *SessionHandler) Create(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := middleware.WorkspaceIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated workspace")
		return
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated user")
		return
	}

	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	ticketID := chi.URLParam(r, "id")
	session, err := scanSession(h.db.QueryRow(r.Context(), `
		INSERT INTO sessions (ticket_id, user_id, title, goal)
		SELECT id, $3, $4, $5
		FROM tickets
		WHERE id = $1 AND workspace_id = $2
		RETURNING id::text, ticket_id::text, user_id::text, title, goal, status, summary, created_at, updated_at, completed_at
	`,
		ticketID,
		workspaceID,
		userID,
		req.Title,
		cleanOptionalString(req.Goal),
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "ticket not found")
			return
		}
		if isInvalidUUID(err) {
			writeError(w, http.StatusBadRequest, "invalid ticket id")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}

	writeJSON(w, http.StatusCreated, session)
}

func (h *SessionHandler) ListForTicket(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := middleware.WorkspaceIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated workspace")
		return
	}

	ticketID := chi.URLParam(r, "id")
	rows, err := h.db.Query(r.Context(), `
		SELECT s.id::text, s.ticket_id::text, s.user_id::text, s.title, s.goal, s.status, s.summary, s.created_at, s.updated_at, s.completed_at
		FROM sessions s
		JOIN tickets t ON t.id = s.ticket_id
		WHERE t.id = $1 AND t.workspace_id = $2
		ORDER BY s.created_at DESC, s.id DESC
	`, ticketID, workspaceID)
	if err != nil {
		if isInvalidUUID(err) {
			writeError(w, http.StatusBadRequest, "invalid ticket id")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not list sessions")
		return
	}
	defer rows.Close()

	sessions := []sessionResponse{}
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not read session")
			return
		}
		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not read sessions")
		return
	}

	writeJSON(w, http.StatusOK, sessions)
}

func (h *SessionHandler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := middleware.WorkspaceIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated workspace")
		return
	}

	sessionID := chi.URLParam(r, "id")
	session, err := scanSession(h.db.QueryRow(r.Context(), `
		SELECT s.id::text, s.ticket_id::text, s.user_id::text, s.title, s.goal, s.status, s.summary, s.created_at, s.updated_at, s.completed_at
		FROM sessions s
		JOIN tickets t ON t.id = s.ticket_id
		WHERE s.id = $1 AND t.workspace_id = $2
	`, sessionID, workspaceID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if isInvalidUUID(err) {
			writeError(w, http.StatusBadRequest, "invalid session id")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not get session")
		return
	}

	writeJSON(w, http.StatusOK, session)
}

func (h *SessionHandler) Update(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := middleware.WorkspaceIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated workspace")
		return
	}

	var req updateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Status == nil && req.Summary == nil {
		writeError(w, http.StatusBadRequest, "status or summary is required")
		return
	}

	var status *string
	if req.Status != nil {
		cleanStatus := strings.ToLower(strings.TrimSpace(*req.Status))
		if !isValidSessionStatus(cleanStatus) {
			writeError(w, http.StatusBadRequest, "status must be one of active, completed, archived")
			return
		}
		status = &cleanStatus
	}

	summaryProvided := req.Summary != nil
	summary := cleanOptionalString(req.Summary)

	sessionID := chi.URLParam(r, "id")
	session, err := scanSession(h.db.QueryRow(r.Context(), `
		UPDATE sessions s
		SET
			status = COALESCE($3::text, s.status),
			summary = CASE WHEN $4 THEN $5::text ELSE s.summary END,
			completed_at = CASE
				WHEN $3::text = 'completed' THEN COALESCE(s.completed_at, now())
				WHEN $3::text IS NOT NULL AND $3::text <> 'completed' THEN NULL
				ELSE s.completed_at
			END,
			updated_at = now()
		FROM tickets t
		WHERE s.ticket_id = t.id
			AND s.id = $1
			AND t.workspace_id = $2
		RETURNING s.id::text, s.ticket_id::text, s.user_id::text, s.title, s.goal, s.status, s.summary, s.created_at, s.updated_at, s.completed_at
	`, sessionID, workspaceID, status, summaryProvided, summary))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if isInvalidUUID(err) {
			writeError(w, http.StatusBadRequest, "invalid session id")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not update session")
		return
	}

	writeJSON(w, http.StatusOK, session)
}

func scanSession(scanner rowScanner) (sessionResponse, error) {
	var session sessionResponse
	var goal sql.NullString
	var summary sql.NullString
	var completedAt sql.NullTime

	err := scanner.Scan(
		&session.ID,
		&session.TicketID,
		&session.UserID,
		&session.Title,
		&goal,
		&session.Status,
		&summary,
		&session.CreatedAt,
		&session.UpdatedAt,
		&completedAt,
	)
	if err != nil {
		return sessionResponse{}, err
	}

	session.Goal = stringPointerFromNull(goal)
	session.Summary = stringPointerFromNull(summary)
	if completedAt.Valid {
		session.CompletedAt = &completedAt.Time
	}

	return session, nil
}

func isValidSessionStatus(status string) bool {
	switch status {
	case "active", "completed", "archived":
		return true
	default:
		return false
	}
}
