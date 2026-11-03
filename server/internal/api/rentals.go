package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"sklad/server/internal/auth"
	"sklad/server/internal/jobs"
)

// RentalAnnouncer posts the rental changes of one save. It must not block: the
// save has already committed. *jobs.Scheduler implements it.
type RentalAnnouncer interface {
	AnnounceRentals(changes []jobs.RentalChange, actor string)
}

// rentalChange reports whether a save started or changed a rental: the item is
// in a status other than Feasibility afterwards, and its status, renter,
// dates or note changed. A return (→ Feasibility) and Admin edits of the
// item record itself (name, SN, type) are not announced.
func rentalChange(before itemState, after Item) (jobs.RentalChange, bool) {
	status := deref(after.Status)
	if status == "" || strings.EqualFold(status, "Feasibility") {
		return jobs.RentalChange{}, false
	}
	previous := deref(before.Status)
	changed := previous != status ||
		!sameID(before.PersonnelID, after.PersonnelID) ||
		deref(nullString(before.SentDate)) != deref(after.SentDate) ||
		deref(nullString(before.ReturnDate)) != deref(after.ReturnDate) ||
		before.Note != after.Note
	if !changed {
		return jobs.RentalChange{}, false
	}
	itemType := deref(after.TypeLabel)
	if itemType == "" {
		itemType = deref(after.Type)
	}
	return jobs.RentalChange{
		AssetTag:       after.AssetTag,
		Name:           after.Name,
		SN:             after.SN,
		Type:           itemType,
		PreviousStatus: previous,
		Status:         status,
		StatusColor:    deref(after.StatusColor),
		Renter:         deref(after.PersonnelName),
		SentDate:       deref(after.SentDate),
		ReturnDate:     deref(after.ReturnDate),
		Note:           after.Note,
	}, true
}

// announceRentals hands the rental changes of a committed save to the
// announcer, attributed to the signed-in Admin or the anonymous Editor.
func (a *API) announceRentals(r *http.Request, changes []jobs.RentalChange) {
	if a.rentals == nil || len(changes) == 0 {
		return
	}
	actor := ""
	if u := auth.UserFrom(r.Context()); u != nil {
		actor = u.Username
	}
	a.rentals.AnnounceRentals(changes, actor)
}

func sameID(before sql.NullInt64, after *int64) bool {
	if !before.Valid || after == nil {
		return !before.Valid && after == nil
	}
	return before.Int64 == *after
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func isAdmin(r *http.Request) bool {
	u := auth.UserFrom(r.Context())
	return u != nil && u.Role == auth.RoleAdmin
}
func nullableInt(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}

// Public writes are JSON-only and same-origin in browsers. Authenticated Admin
// requests retain session CSRF protection. A closed field allowlist also rejects
// null/unknown properties instead of silently accepting administrative fields.
func (a *API) rentalWriteGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if media != "application/json" {
			Error(w, r, 400, CodeBadRequest, "application/json required")
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			Error(w, r, 403, CodeForbidden, "same-origin request required")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, e := url.Parse(origin)
			if e != nil || (u.Host != r.Host && strings.TrimRight(origin, "/") != strings.TrimRight(a.cfg.BaseURL, "/")) {
				Error(w, r, 403, CodeForbidden, "same-origin request required")
				return
			}
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes))
		if err != nil {
			Error(w, r, 413, CodePayloadTooLarge, "request too large")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(raw, &fields); err != nil {
			DecodeError(w, r, err)
			return
		}
		allowed := map[string]bool{"version": true, "status": true, "sent_date": true, "return_date": true, "personnel_id": true, "note": true}
		if strings.HasSuffix(r.URL.Path, "/bulk") {
			for key := range fields {
				if key != "items" && key != "patch" {
					Error(w, r, 400, CodeValidationFailed, "unsupported bulk field: "+key)
					return
				}
			}
			patchJSON := fields["patch"]
			fields = nil
			if err = json.Unmarshal(patchJSON, &fields); err != nil {
				DecodeError(w, r, err)
				return
			}
			delete(allowed, "version")
			allowed["clear_sent_date"] = true
			allowed["clear_return_date"] = true
		} else if isAdmin(r) {
			allowed["name"] = true
			allowed["sn"] = true
			allowed["type"] = true
		}
		for key, value := range fields {
			if !allowed[key] {
				Error(w, r, 403, CodeForbidden, "unsupported rental field: "+key)
				return
			}
			if string(value) == "null" {
				Error(w, r, 400, CodeValidationFailed, key+" cannot be null; use an empty string or personnel_id 0 to clear")
				return
			}
		}
		if isAdmin(r) {
			a.auth.CSRF(next).ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// One validator for individual, replacement, creation and transactional bulk writes.
func (a *API) validateRental(r *http.Request, tx *sql.Tx, before itemState, target itemTarget, ch itemChanges) error {
	fail := func(message string) error {
		return &validationError{details: []string{before.AssetTag + ": " + message}}
	}
	if before.Archived && !isAdmin(r) {
		return fail("archived equipment cannot be rented")
	}
	var status string
	if !target.StatusID.Valid {
		return fail("status is required")
	}
	if err := tx.QueryRowContext(r.Context(), `SELECT value FROM statuses WHERE id=?`, target.StatusID).Scan(&status); err != nil {
		return err
	}
	feasibility := strings.EqualFold(status, "Feasibility")
	wasFeasibility := before.Status != nil && strings.EqualFold(*before.Status, "Feasibility")
	if feasibility {
		if before.ID != 0 && !wasFeasibility && !isAdmin(r) {
			return fail("only an Admin can confirm physical return by selecting Feasibility")
		}
		if target.SentDate != before.SentDate || target.ReturnDate != before.ReturnDate || target.PersonnelID != before.PersonnelID || target.Note != before.Note {
			return fail("rental fields are locked in Feasibility; select another status before editing")
		}
		return nil
	}
	if !target.PersonnelID.Valid {
		return fail("Personnel renting the device is required")
	}
	var active, customer bool
	err := tx.QueryRowContext(r.Context(), `SELECT active,is_customer FROM personnel WHERE id=?`, target.PersonnelID).Scan(&active, &customer)
	if err == sql.ErrNoRows {
		return fail("select an existing person")
	}
	if err != nil {
		return err
	}
	// An inactive reference can remain on an existing rental. Starting a new
	// rental or explicitly assigning an inactive person is forbidden.
	if !active && (before.PersonnelID != target.PersonnelID || wasFeasibility || before.ID == 0) {
		return fail("this person is inactive; select an active person")
	}
	if customer && strings.TrimSpace(target.Note) == "" {
		return fail("Customer requires a Note containing the customer's name")
	}
	return nil
}
