package api

// The unauthenticated operational endpoints live in this file. They are mounted
// outside the session and CSRF middleware so that health probes, TLS checks and
// monitoring work without credentials.

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

const healthCheckTimeout = 2 * time.Second

// healthResponse is the GET /api/v1/health body (the Health schema in
// openapi/openapi.yaml).
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Time    string `json:"time"`
}

// versionResponse is the GET /api/v1/version body (the Version schema).
type versionResponse struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
}

// mountSystemRoutes registers the operational endpoints. They are deliberately
// unauthenticated and must never expose data beyond build metadata.
func (a *API) mountSystemRoutes(r chi.Router) {
	r.Get("/health", a.handleHealth)
	r.Get("/version", a.handleVersion)
}

// handleHealth implements GET /api/v1/health: 200 while every dependency is
// reachable, 503 as soon as one is not. The body keeps the documented shape in
// both cases; the reason is logged with the request ID.
func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	status := http.StatusOK
	state := "ok"
	if a.health != nil {
		ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
		defer cancel()
		if err := a.health(ctx); err != nil {
			status = http.StatusServiceUnavailable
			state = "degraded"
			a.log.Error("health check failed",
				"error", err,
				"request_id", RequestIDFrom(r.Context()),
			)
		}
	}

	WriteJSON(w, status, healthResponse{
		Status:  state,
		Version: a.version.Version,
		Time:    time.Now().UTC().Format(time.RFC3339),
	})
}

// handleVersion implements GET /api/v1/version.
func (a *API) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, versionResponse{
		Version:   a.version.Version,
		Commit:    a.version.Commit,
		BuildTime: a.version.BuildTime,
	})
}
