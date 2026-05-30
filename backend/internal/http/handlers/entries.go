package handlers

import (
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

type EntryHandler struct {
	db *pgxpool.Pool
}

func NewEntryHandler(db *pgxpool.Pool) *EntryHandler {
	return &EntryHandler{db: db}
}

type createEntryRequest struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type updateEntryRequest struct {
	Type    *string `json:"type"`
	Content *string `json:"content"`
}

type entryResponse struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	Type      string    `json:"type"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (h *EntryHandler) Create(w http.ResponseWriter, r *http.Request) {
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

	var req createEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	entryType := strings.ToLower(strings.TrimSpace(req.Type))
	content := strings.TrimSpace(req.Content)

	if !isValidEntryType(entryType) {
		writeError(w, http.StatusBadRequest, "type must be one of decision, tried_failed, bug_fixed, warning, prompt, context")
		return
	}

	if content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	sessionID := chi.URLParam(r, "id")
	entry, err := scanEntry(h.db.QueryRow(r.Context(), `
		INSERT INTO context_entries (session_id, user_id, type, content)
		SELECT s.id, $3, $4, $5
		FROM sessions s
		JOIN tickets t ON t.id = s.ticket_id
		WHERE s.id = $1 AND t.workspace_id = $2
		RETURNING id::text, session_id::text, user_id::text, type, content, created_at, updated_at
	`, sessionID, workspaceID, userID, entryType, content))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if isInvalidUUID(err) {
			writeError(w, http.StatusBadRequest, "invalid session id")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not create entry")
		return
	}

	writeJSON(w, http.StatusCreated, entry)
}

func (h *EntryHandler) Update(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := middleware.WorkspaceIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated workspace")
		return
	}

	var req updateEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Type == nil && req.Content == nil {
		writeError(w, http.StatusBadRequest, "type or content is required")
		return
	}

	var entryType *string
	if req.Type != nil {
		cleanType := strings.ToLower(strings.TrimSpace(*req.Type))
		if !isValidEntryType(cleanType) {
			writeError(w, http.StatusBadRequest, "type must be one of decision, tried_failed, bug_fixed, warning, prompt, context")
			return
		}
		entryType = &cleanType
	}

	var content *string
	if req.Content != nil {
		cleanContent := strings.TrimSpace(*req.Content)
		if cleanContent == "" {
			writeError(w, http.StatusBadRequest, "content cannot be empty")
			return
		}
		content = &cleanContent
	}

	entryID := chi.URLParam(r, "id")
	entry, err := scanEntry(h.db.QueryRow(r.Context(), `
		UPDATE context_entries ce
		SET
			type = COALESCE($3::text, ce.type),
			content = COALESCE($4::text, ce.content),
			updated_at = now()
		FROM sessions s
		JOIN tickets t ON t.id = s.ticket_id
		WHERE ce.session_id = s.id
			AND ce.id = $1
			AND t.workspace_id = $2
		RETURNING ce.id::text, ce.session_id::text, ce.user_id::text, ce.type, ce.content, ce.created_at, ce.updated_at
	`, entryID, workspaceID, entryType, content))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "entry not found")
			return
		}
		if isInvalidUUID(err) {
			writeError(w, http.StatusBadRequest, "invalid entry id")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not update entry")
		return
	}

	writeJSON(w, http.StatusOK, entry)
}

func (h *EntryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := middleware.WorkspaceIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated workspace")
		return
	}

	entryID := chi.URLParam(r, "id")
	var deletedID string
	err := h.db.QueryRow(r.Context(), `
		DELETE FROM context_entries ce
		USING sessions s, tickets t
		WHERE ce.session_id = s.id
			AND s.ticket_id = t.id
			AND ce.id = $1
			AND t.workspace_id = $2
		RETURNING ce.id::text
	`, entryID, workspaceID).Scan(&deletedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "entry not found")
			return
		}
		if isInvalidUUID(err) {
			writeError(w, http.StatusBadRequest, "invalid entry id")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not delete entry")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func scanEntry(scanner rowScanner) (entryResponse, error) {
	var entry entryResponse

	err := scanner.Scan(
		&entry.ID,
		&entry.SessionID,
		&entry.UserID,
		&entry.Type,
		&entry.Content,
		&entry.CreatedAt,
		&entry.UpdatedAt,
	)
	if err != nil {
		return entryResponse{}, err
	}

	return entry, nil
}

func isValidEntryType(entryType string) bool {
	switch entryType {
	case "decision", "tried_failed", "bug_fixed", "warning", "prompt", "context":
		return true
	default:
		return false
	}
}
