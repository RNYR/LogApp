package api

// Transactional bulk update: the scan-buffer workflow. A client scans many
// labels, reviews the buffer, and applies one patch to all of them at once.
// Either every listed item is updated or none is — a single stale version or
// invalid value rolls the whole batch back (schema.md, "Concurrency & integrity
// rules", rule 2).

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"sklad/server/internal/audit"
	"sklad/server/internal/auth"
)

// bulkItemRef identifies one buffered item and the version the client saw.
type bulkItemRef struct {
	AssetTag string `json:"asset_tag"`
	Version  *int64 `json:"version"`
}

// bulkPatch is the change applied to every listed item. Dates can be set with
// sent_date/return_date or cleared explicitly with clear_sent_date/
// clear_return_date; an empty date string clears as well.
type bulkPatch struct {
	PersonnelID     *int64  `json:"personnel_id"`
	Status          *string `json:"status"`
	Note            *string `json:"note"`
	SentDate        *string `json:"sent_date"`
	ReturnDate      *string `json:"return_date"`
	ClearSentDate   bool    `json:"clear_sent_date"`
	ClearReturnDate bool    `json:"clear_return_date"`
}

// bulkRequest is the body of POST /api/v1/items/bulk.
type bulkRequest struct {
	Items []bulkItemRef `json:"items"`
	Patch bulkPatch     `json:"patch"`
}

// bulkResponse is the body of a successful bulk update.
type bulkResponse struct {
	Updated      int    `json:"updated"`
	UpdatedItems []Item `json:"updated_items"`
}

// mountBulkRoutes registers the bulk endpoint on the protected router.
func (a *API) mountBulkRoutes(r chi.Router) {
	r.Post("/items/bulk", a.handleBulkUpdate)
}

// handleBulkUpdate implements POST /api/v1/items/bulk.
func (a *API) handleBulkUpdate(w http.ResponseWriter, r *http.Request) {
	var body bulkRequest
	if err := DecodeJSON(w, r, &body); err != nil {
		DecodeError(w, r, err)
		return
	}
	if len(body.Items) == 0 {
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, "items must contain at least one entry")
		return
	}
	refs, err := normalizeBulkRefs(body.Items)
	if err != nil {
		a.writeItemError(w, r, err)
		return
	}
	changes, err := body.Patch.toChanges()
	if err != nil {
		a.writeItemError(w, r, err)
		return
	}
	if changes.empty() {
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, "patch must change at least one field")
		return
	}

	locale := requestLocale(r)
	var updated []Item

	err = a.withTx(r.Context(), func(tx *sql.Tx) error {
		updated = make([]Item, 0, len(refs))
		for _, ref := range refs {
			item, err := a.getItemByTag(r.Context(), tx, ref.AssetTag, locale)
			if err != nil {
				return err
			}
			st, err := a.loadItemState(r.Context(), tx, item.ID)
			if err != nil {
				return err
			}
			if st.Version != *ref.Version {
				// Any conflict aborts the whole batch.
				return &versionConflictError{
					entity: "item", ref: st.AssetTag, expected: *ref.Version, current: st.Version,
				}
			}
			target, err := a.apply(r.Context(), tx, st, changes)
			if err != nil {
				return err
			}
			if details := validateTarget(target); len(details) > 0 {
				return &validationError{details: details}
			}
			if err := a.validateRental(r, tx, st, target, changes); err != nil {
				return err
			}
			after, err := a.updateItem(r.Context(), tx, st, target, auth.UserFrom(r.Context()), locale)
			if err != nil {
				return err
			}
			// schema.md: every changed row gets its own bulk_update audit entry.
			if err := a.recordAudit(r.Context(), tx, a.auditRecord(
				r, audit.ActionBulkUpdate, audit.EntityItem, after.AssetTag, st.snapshot(), after.snapshot(),
			)); err != nil {
				return err
			}
			updated = append(updated, after)
		}
		return nil
	})
	if err != nil {
		a.writeItemError(w, r, err)
		return
	}

	writeData(w, http.StatusOK, bulkResponse{Updated: len(updated), UpdatedItems: updated})
}

// toChanges converts the bulk patch into the shared internal patch.
func (p bulkPatch) toChanges() (itemChanges, error) {
	if p.ClearSentDate && p.SentDate != nil && strings.TrimSpace(*p.SentDate) != "" {
		return itemChanges{}, &validationError{details: []string{
			"sent_date and clear_sent_date contradict each other"}}
	}
	if p.ClearReturnDate && p.ReturnDate != nil && strings.TrimSpace(*p.ReturnDate) != "" {
		return itemChanges{}, &validationError{details: []string{
			"return_date and clear_return_date contradict each other"}}
	}

	changes := itemChanges{
		PersonnelID: p.PersonnelID,
		Status:      p.Status,
		Note:        p.Note,
		SentDate:    p.SentDate,
		ReturnDate:  p.ReturnDate,
	}
	if p.ClearSentDate || (p.SentDate != nil && strings.TrimSpace(*p.SentDate) == "") {
		changes.SentDate = nil
		changes.ClearSent = true
	}
	if p.ClearReturnDate || (p.ReturnDate != nil && strings.TrimSpace(*p.ReturnDate) == "") {
		changes.ReturnDate = nil
		changes.ClearReturn = true
	}
	return changes, nil
}

// normalizeBulkRefs trims and validates the referenced asset tags and rejects
// duplicates, which would otherwise apply the same patch twice.
func normalizeBulkRefs(items []bulkItemRef) ([]bulkItemRef, error) {
	out := make([]bulkItemRef, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for index, ref := range items {
		tag := normalizeCode(ref.AssetTag)
		if problem := assetTagProblem(tag); problem != "" {
			return nil, &validationError{details: []string{fmt.Sprintf("items[%d]: %s", index, problem)}}
		}
		if ref.Version == nil || *ref.Version < 1 {
			return nil, &validationError{details: []string{
				fmt.Sprintf("items[%d]: version is required and must be a positive integer", index)}}
		}
		if _, ok := seen[tag]; ok {
			return nil, &validationError{details: []string{
				fmt.Sprintf("items[%d]: duplicate asset_tag %q", index, tag)}}
		}
		seen[tag] = struct{}{}
		out = append(out, bulkItemRef{AssetTag: tag, Version: ref.Version})
	}
	return out, nil
}
