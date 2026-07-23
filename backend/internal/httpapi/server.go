package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

type Server struct {
	db *sql.DB
}

type healthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
}

func New(db *sql.DB) http.Handler {
	server := &Server{db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", server.health)
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()

	response := healthResponse{Status: "healthy", Database: "connected"}
	statusCode := http.StatusOK
	if err := s.db.PingContext(ctx); err != nil {
		response.Status = "unhealthy"
		response.Database = "unavailable"
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(response)
}
