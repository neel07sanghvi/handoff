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
	authHandler := handlers.NewAuthHandler(dbpool, cfg.JWTSecret)
	ticketHandler := handlers.NewTicketHandler(dbpool)
	sessionHandler := handlers.NewSessionHandler(dbpool)

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
		r.Post("/tickets", ticketHandler.Create)
		r.Get("/tickets", ticketHandler.List)
		r.Get("/tickets/{id}", ticketHandler.Get)
		r.Post("/tickets/{id}/sessions", sessionHandler.Create)
		r.Get("/sessions/{id}", sessionHandler.Get)
		r.Patch("/sessions/{id}", sessionHandler.Update)
	})

	log.Println("API running on http://localhost:" + cfg.Port)

	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatal(err)
	}
}
