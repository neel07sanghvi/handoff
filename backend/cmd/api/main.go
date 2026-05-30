package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/neelsanghvi/handoff/backend/internal/config"
	"github.com/neelsanghvi/handoff/backend/internal/db"
	"github.com/neelsanghvi/handoff/backend/internal/http/handlers"
	"github.com/neelsanghvi/handoff/backend/internal/middleware"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	dbpool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer dbpool.Close()

	r := chi.NewRouter()
	r.Use(allowLocalFrontend)
	authHandler := handlers.NewAuthHandler(dbpool, cfg.JWTSecret)
	ticketHandler := handlers.NewTicketHandler(dbpool)
	sessionHandler := handlers.NewSessionHandler(dbpool)
	entryHandler := handlers.NewEntryHandler(dbpool)
	handoffHandler := handlers.NewHandoffHandler(dbpool)
	searchHandler := handlers.NewSearchHandler(dbpool)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":   "ok",
			"database": "connected",
		})
	})

	r.Post("/api/auth/register", authHandler.Register)
	r.Post("/api/auth/login", authHandler.Login)

	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.Auth(cfg.JWTSecret))
		r.Get("/me", authHandler.Me)
		r.Get("/search", searchHandler.Search)
		r.Post("/tickets", ticketHandler.Create)
		r.Get("/tickets", ticketHandler.List)
		r.Get("/tickets/{id}", ticketHandler.Get)
		r.Get("/tickets/{id}/handoff", handoffHandler.Generate)
		r.Post("/tickets/{id}/sessions", sessionHandler.Create)
		r.Get("/tickets/{id}/sessions", sessionHandler.ListForTicket)
		r.Get("/sessions/{id}", sessionHandler.Get)
		r.Patch("/sessions/{id}", sessionHandler.Update)
		r.Post("/sessions/{id}/entries", entryHandler.Create)
		r.Get("/sessions/{id}/entries", entryHandler.ListForSession)
		r.Patch("/entries/{id}", entryHandler.Update)
		r.Delete("/entries/{id}", entryHandler.Delete)
	})

	log.Println("API running on http://localhost:" + cfg.Port)

	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatal(err)
	}
}

func allowLocalFrontend(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "http://127.0.0.1:5173" || origin == "http://localhost:5173" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
