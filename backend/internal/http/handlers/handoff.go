package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neelsanghvi/handoff/backend/internal/middleware"
)

type HandoffHandler struct {
	db *pgxpool.Pool
}

func NewHandoffHandler(db *pgxpool.Pool) *HandoffHandler {
	return &HandoffHandler{db: db}
}

type handoffResponse struct {
	TicketID string `json:"ticket_id"`
	Content  string `json:"content"`
}

type handoffTicket struct {
	ID         string
	ExternalID *string
	Title      string
}

type handoffEntry struct {
	CreatedAt time.Time
	UserName  string
	Type      string
	Content   string
}

func (h *HandoffHandler) Generate(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := middleware.WorkspaceIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated workspace")
		return
	}

	ticketID := chi.URLParam(r, "id")
	ticket, err := h.findTicket(r, ticketID, workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "ticket not found")
			return
		}
		if isInvalidUUID(err) {
			writeError(w, http.StatusBadRequest, "invalid ticket id")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not get ticket")
		return
	}

	entries, err := h.findEntries(r, ticketID, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate handoff")
		return
	}

	writeJSON(w, http.StatusOK, handoffResponse{
		TicketID: ticket.ID,
		Content:  buildHandoffContent(ticket, entries),
	})
}

func (h *HandoffHandler) findTicket(r *http.Request, ticketID string, workspaceID string) (handoffTicket, error) {
	var ticket handoffTicket
	var externalID sql.NullString

	err := h.db.QueryRow(r.Context(), `
		SELECT id::text, external_id, title
		FROM tickets
		WHERE id = $1 AND workspace_id = $2
	`, ticketID, workspaceID).Scan(
		&ticket.ID,
		&externalID,
		&ticket.Title,
	)
	if err != nil {
		return handoffTicket{}, err
	}

	ticket.ExternalID = stringPointerFromNull(externalID)
	return ticket, nil
}

func (h *HandoffHandler) findEntries(r *http.Request, ticketID string, workspaceID string) ([]handoffEntry, error) {
	rows, err := h.db.Query(r.Context(), `
		SELECT ce.created_at, u.name, ce.type, ce.content
		FROM context_entries ce
		JOIN sessions s ON s.id = ce.session_id
		JOIN tickets t ON t.id = s.ticket_id
		JOIN users u ON u.id = ce.user_id
		WHERE t.id = $1 AND t.workspace_id = $2
		ORDER BY ce.created_at ASC, ce.id ASC
	`, ticketID, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := []handoffEntry{}
	for rows.Next() {
		var entry handoffEntry
		if err := rows.Scan(&entry.CreatedAt, &entry.UserName, &entry.Type, &entry.Content); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func buildHandoffContent(ticket handoffTicket, entries []handoffEntry) string {
	ticketLabel := ticket.ID
	if ticket.ExternalID != nil {
		ticketLabel = *ticket.ExternalID
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "=== HANDOFF CONTEXT: %s \u2014 %s ===\n\n", ticketLabel, ticket.Title)

	for _, entry := range entries {
		fmt.Fprintf(
			&builder,
			"[%s \u2014 %s]\n%s:\n%s\n\n",
			entry.CreatedAt.Format("2006-01-02 15:04"),
			entry.UserName,
			entryTypeLabel(entry.Type),
			entry.Content,
		)
	}

	builder.WriteString("=== END CONTEXT ===")
	return builder.String()
}

func entryTypeLabel(entryType string) string {
	switch entryType {
	case "decision":
		return "DECISION"
	case "tried_failed":
		return "TRIED & FAILED"
	case "bug_fixed":
		return "BUG FIXED"
	case "warning":
		return "WARNING"
	case "prompt":
		return "PROMPT"
	default:
		return "CONTEXT"
	}
}
