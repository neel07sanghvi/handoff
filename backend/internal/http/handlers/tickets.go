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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neelsanghvi/handoff/backend/internal/middleware"
)

type TicketHandler struct {
	db *pgxpool.Pool
}

func NewTicketHandler(db *pgxpool.Pool) *TicketHandler {
	return &TicketHandler{db: db}
}

type createTicketRequest struct {
	ExternalID  *string `json:"external_id"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	URL         *string `json:"url"`
	Source      string  `json:"source"`
}

type ticketResponse struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	ExternalID  *string   `json:"external_id"`
	Title       string    `json:"title"`
	Description *string   `json:"description"`
	URL         *string   `json:"url"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (h *TicketHandler) Create(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := middleware.WorkspaceIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated workspace")
		return
	}

	var req createTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	req.Title = strings.TrimSpace(req.Title)
	req.Source = strings.ToLower(strings.TrimSpace(req.Source))
	if req.Source == "" {
		req.Source = "manual"
	}

	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	if !isValidTicketSource(req.Source) {
		writeError(w, http.StatusBadRequest, "source must be one of github, linear, jira, manual")
		return
	}

	ticket, err := scanTicket(h.db.QueryRow(r.Context(), `
		INSERT INTO tickets (workspace_id, external_id, title, description, url, source)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text, workspace_id::text, external_id, title, description, url, source, created_at, updated_at
	`,
		workspaceID,
		cleanOptionalString(req.ExternalID),
		req.Title,
		cleanOptionalString(req.Description),
		cleanOptionalString(req.URL),
		req.Source,
	))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create ticket")
		return
	}

	writeJSON(w, http.StatusCreated, ticket)
}

func (h *TicketHandler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := middleware.WorkspaceIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated workspace")
		return
	}

	rows, err := h.db.Query(r.Context(), `
		SELECT id::text, workspace_id::text, external_id, title, description, url, source, created_at, updated_at
		FROM tickets
		WHERE workspace_id = $1
		ORDER BY created_at DESC
	`, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list tickets")
		return
	}
	defer rows.Close()

	tickets := []ticketResponse{}
	for rows.Next() {
		ticket, err := scanTicket(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not read ticket")
			return
		}
		tickets = append(tickets, ticket)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not read tickets")
		return
	}

	writeJSON(w, http.StatusOK, tickets)
}

func (h *TicketHandler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := middleware.WorkspaceIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing authenticated workspace")
		return
	}

	ticketID := chi.URLParam(r, "id")
	ticket, err := scanTicket(h.db.QueryRow(r.Context(), `
		SELECT id::text, workspace_id::text, external_id, title, description, url, source, created_at, updated_at
		FROM tickets
		WHERE id = $1 AND workspace_id = $2
	`, ticketID, workspaceID))
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

	writeJSON(w, http.StatusOK, ticket)
}

func scanTicket(scanner rowScanner) (ticketResponse, error) {
	var ticket ticketResponse
	var externalID sql.NullString
	var description sql.NullString
	var url sql.NullString

	err := scanner.Scan(
		&ticket.ID,
		&ticket.WorkspaceID,
		&externalID,
		&ticket.Title,
		&description,
		&url,
		&ticket.Source,
		&ticket.CreatedAt,
		&ticket.UpdatedAt,
	)
	if err != nil {
		return ticketResponse{}, err
	}

	ticket.ExternalID = stringPointerFromNull(externalID)
	ticket.Description = stringPointerFromNull(description)
	ticket.URL = stringPointerFromNull(url)

	return ticket, nil
}

func isValidTicketSource(source string) bool {
	switch source {
	case "github", "linear", "jira", "manual":
		return true
	default:
		return false
	}
}

func cleanOptionalString(value *string) *string {
	if value == nil {
		return nil
	}

	cleaned := strings.TrimSpace(*value)
	if cleaned == "" {
		return nil
	}

	return &cleaned
}

func stringPointerFromNull(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}

	return &value.String
}

func isInvalidUUID(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}
