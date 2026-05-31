package handlers

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neelsanghvi/handoff/backend/internal/middleware"
)

type SearchHandler struct {
	db *pgxpool.Pool
}

func NewSearchHandler(db *pgxpool.Pool) *SearchHandler {
	return &SearchHandler{db: db}
}

type searchResultResponse struct {
	ID               string    `json:"id"`
	SessionID        string    `json:"session_id"`
	TicketID         string    `json:"ticket_id"`
	TicketExternalID *string   `json:"ticket_external_id"`
	TicketTitle      string    `json:"ticket_title"`
	UserID           string    `json:"user_id"`
	UserName         string    `json:"user_name"`
	Type             string    `json:"type"`
	Content          string    `json:"content"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := middleware.WorkspaceIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated workspace")
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}

	entryType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))
	if entryType != "" && !isValidEntryType(entryType) {
		writeError(w, http.StatusBadRequest, "type must be one of decision, tried_failed, bug_fixed, warning, prompt, context")
		return
	}

	rows, err := h.db.Query(r.Context(), `
		SELECT
			ce.id::text,
			ce.session_id::text,
			s.ticket_id::text,
			t.external_id,
			t.title,
			ce.user_id::text,
			u.name,
			ce.type,
			ce.content,
			ce.created_at,
			ce.updated_at
		FROM context_entries ce
		JOIN sessions s ON s.id = ce.session_id
		JOIN tickets t ON t.id = s.ticket_id
		JOIN users u ON u.id = ce.user_id
		WHERE t.workspace_id = $1
			AND (
				ce.content ILIKE '%' || $2 || '%'
				OR t.title ILIKE '%' || $2 || '%'
				OR COALESCE(t.external_id, '') ILIKE '%' || $2 || '%'
				OR COALESCE(t.description, '') ILIKE '%' || $2 || '%'
				OR s.title ILIKE '%' || $2 || '%'
				OR COALESCE(s.goal, '') ILIKE '%' || $2 || '%'
				OR COALESCE(s.summary, '') ILIKE '%' || $2 || '%'
			)
			AND ($3::text = '' OR ce.type = $3)
		ORDER BY ce.created_at DESC, ce.id DESC
	`, workspaceID, query, entryType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not search entries")
		return
	}
	defer rows.Close()

	results := []searchResultResponse{}
	for rows.Next() {
		var result searchResultResponse
		var ticketExternalID sql.NullString

		if err := rows.Scan(
			&result.ID,
			&result.SessionID,
			&result.TicketID,
			&ticketExternalID,
			&result.TicketTitle,
			&result.UserID,
			&result.UserName,
			&result.Type,
			&result.Content,
			&result.CreatedAt,
			&result.UpdatedAt,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "could not read search result")
			return
		}

		result.TicketExternalID = stringPointerFromNull(ticketExternalID)
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not read search results")
		return
	}

	writeJSON(w, http.StatusOK, results)
}
