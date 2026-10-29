package api

import (
	"database/sql"
	"fmt"
	"github.com/go-chi/chi/v5"
	"net/http"
	"sklad/server/internal/auth"
	"strconv"
	"strings"
	"time"
)

type Person struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Active     bool   `json:"active"`
	IsCustomer bool   `json:"is_customer"`
	Version    int64  `json:"version"`
}

func (a *API) mountPersonnelRoutes(r chi.Router) {
	r.With(a.auth.RequireRole(auth.RoleAdmin)).Post("/personnel", a.handleWritePersonnel)
	r.With(a.auth.RequireRole(auth.RoleAdmin)).Patch("/personnel/{personId}", a.handleWritePersonnel)
}
func (a *API) handleListPersonnel(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,name,active,is_customer,version FROM personnel ORDER BY is_customer,name COLLATE NOCASE,id`)
	if err != nil {
		InternalError(w, r, a.log, err)
		return
	}
	defer rows.Close()
	list := []Person{}
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.ID, &p.Name, &p.Active, &p.IsCustomer, &p.Version); err != nil {
			InternalError(w, r, a.log, err)
			return
		}
		list = append(list, p)
	}
	if err := rows.Err(); err != nil {
		InternalError(w, r, a.log, err)
		return
	}
	writeData(w, 200, list)
}
func (a *API) handleWritePersonnel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    *string `json:"name"`
		Active  *bool   `json:"active"`
		Version *int64  `json:"version"`
	}
	if err := DecodeJSON(w, r, &body); err != nil {
		DecodeError(w, r, err)
		return
	}
	var result Person
	err := a.withTx(r.Context(), func(tx *sql.Tx) error {
		now := a.now().UTC().Format(time.RFC3339)
		var before *Person
		if r.Method == http.MethodPost {
			result.Active = true
		} else {
			id, e := strconv.ParseInt(chi.URLParam(r, "personId"), 10, 64)
			if e != nil {
				return &validationError{details: []string{"invalid personnel id"}}
			}
			e = tx.QueryRowContext(r.Context(), `SELECT id,name,active,is_customer,version FROM personnel WHERE id=?`, id).Scan(&result.ID, &result.Name, &result.Active, &result.IsCustomer, &result.Version)
			if e == sql.ErrNoRows {
				return errItemNotFound
			}
			if e != nil {
				return e
			}
			if result.IsCustomer {
				return &validationError{details: []string{"Customer is a permanent directory option"}}
			}
			if body.Version == nil || *body.Version != result.Version {
				return &versionConflictError{entity: "personnel", ref: fmt.Sprint(id), current: result.Version}
			}
			copy := result
			before = &copy
		}
		if body.Name != nil {
			result.Name = strings.TrimSpace(*body.Name)
		}
		if result.Name == "" || len(result.Name) > 255 || strings.EqualFold(result.Name, "Customer") {
			return &validationError{details: []string{"personnel name is required, at most 255 characters; Customer is reserved"}}
		}
		if body.Active != nil {
			result.Active = *body.Active
		}
		if r.Method == http.MethodPost {
			res, e := tx.ExecContext(r.Context(), `INSERT INTO personnel(name,active,created_at,updated_at) VALUES(?,?,?,?)`, result.Name, result.Active, now, now)
			if e != nil {
				return e
			}
			result.ID, e = res.LastInsertId()
			if e != nil {
				return e
			}
			result.Version = 1
		} else {
			_, e := tx.ExecContext(r.Context(), `UPDATE personnel SET name=?,active=?,version=version+1,updated_at=? WHERE id=?`, result.Name, result.Active, now, result.ID)
			if e != nil {
				return e
			}
			result.Version++
		}
		return a.recordAudit(r.Context(), tx, a.auditRecord(r, "personnel_update", "personnel", fmt.Sprint(result.ID), before, result))
	})
	if err != nil {
		a.writeItemError(w, r, err)
		return
	}
	status := 200
	if r.Method == http.MethodPost {
		status = 201
	}
	writeData(w, status, result)
}
