package api

// Inventory items: CRUD with search/filter/pagination, scan resolution, and the
// query/validation/audit helpers shared with options.go, export.go and bulk.go.
//
// Every mutating handler follows the same shape: load the current state inside
// one IMMEDIATE transaction, verify the optimistic-concurrency version, write,
// reload, and append an audit row that commits atomically with the change.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sklad/server/internal/statusnames"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"sklad/server/internal/audit"
	"sklad/server/internal/auth"
)

// Field limits. They mirror what the legacy app accepted while keeping a single
// item row small enough to render comfortably in the SPA table.
const (
	maxAssetTagLen  = 64
	maxNameLen      = 255
	maxSNLen        = 255
	maxNoteLen      = 4000
	maxResolveCodes = 500
	defaultPerPage  = 20
	maxPerPage      = 1000
	// maxPage bounds deep pagination: the offset is derived from it, so a hostile
	// page number can neither overflow nor become a negative SQL OFFSET.
	maxPage = 1_000_000
)

// codeConflict is returned for duplicate keys (409). The version-conflict case
// uses CodeConflict ("version_conflict") from middleware.go; both map to a
// distinct i18n key in the SPA (errors.conflict / errors.versionConflict).
const codeConflict = "conflict"

// ---------------------------------------------------------------------------
// Wire types (openapi/openapi.yaml schemas)
// ---------------------------------------------------------------------------

// Item is the API representation of an inventory row, including the localized
// type/status labels resolved for the request locale.
type Item struct {
	ID             int64   `json:"id"`
	PersonnelID    *int64  `json:"personnel_id"`
	PersonnelName  *string `json:"personnel_name"`
	AssetTag       string  `json:"asset_tag"`
	Name           string  `json:"name"`
	SN             string  `json:"sn"`
	Type           *string `json:"type"`
	TypeLabel      *string `json:"type_label"`
	Status         *string `json:"status"`
	StatusLabel    *string `json:"status_label"`
	StatusColor    *string `json:"status_color"`
	SentDate       *string `json:"sent_date"`
	ReturnDate     *string `json:"return_date"`
	Note           string  `json:"note"`
	Version        int64   `json:"version"`
	Archived       bool    `json:"archived"`
	LabelPrintedAt *string `json:"label_printed_at"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	CreatedBy      *string `json:"created_by"`
	UpdatedBy      *string `json:"updated_by"`
}

// itemListMeta is the pagination block of an item list response.
type itemListMeta struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// itemWriteBody is the JSON body of POST, PUT and PATCH /items. The pointer
// fields distinguish "absent" from "provided"; PUT treats absent fields as
// "reset to the default", PATCH leaves them untouched.
type itemWriteBody struct {
	PersonnelID *int64  `json:"personnel_id"`
	Version     *int64  `json:"version"`
	AssetTag    *string `json:"asset_tag"`
	Name        *string `json:"name"`
	SN          *string `json:"sn"`
	Type        *string `json:"type"`
	Status      *string `json:"status"`
	SentDate    *string `json:"sent_date"`
	ReturnDate  *string `json:"return_date"`
	Note        *string `json:"note"`
}

// resolveRequest is the body of POST /items/resolve: raw scanned CODE128
// payloads, which may differ from the stored tag by case or surrounding space.
type resolveRequest struct {
	Codes []string `json:"codes"`
}

// itemSnapshot is the audit representation of an item (before/after images).
type itemSnapshot struct {
	PersonnelID *int64  `json:"personnel_id"`
	AssetTag    string  `json:"asset_tag"`
	Name        string  `json:"name"`
	SN          string  `json:"sn"`
	Type        *string `json:"type"`
	Status      *string `json:"status"`
	SentDate    *string `json:"sent_date"`
	ReturnDate  *string `json:"return_date"`
	Note        string  `json:"note"`
	Version     int64   `json:"version"`
	Archived    bool    `json:"archived"`
}

// itemState is the mutable part of an items row, loaded for writes: it carries
// the foreign keys (needed to build the UPDATE) and the type/status machine
// keys (needed for the audit snapshot).
type itemState struct {
	PersonnelID sql.NullInt64
	ID          int64
	AssetTag    string
	Name        string
	SN          string
	TypeID      sql.NullInt64
	StatusID    sql.NullInt64
	Type        *string
	Status      *string
	SentDate    sql.NullString
	ReturnDate  sql.NullString
	Note        string
	Version     int64
	Archived    bool
}

// itemChanges is the internal patch applied by PATCH, PUT and bulk. A nil field
// is untouched; Type/Status of "" clears the reference; SentDate/ReturnDate of
// "" clears the date (ClearSent/ClearReturn make that explicit for bulk).
type itemChanges struct {
	PersonnelID *int64
	Name        *string
	SN          *string
	Type        *string
	Status      *string
	Note        *string
	SentDate    *string
	ReturnDate  *string
	ClearSent   bool
	ClearReturn bool
}

// empty reports whether the patch would change nothing at all.
func (ch itemChanges) empty() bool {
	return ch.PersonnelID == nil && ch.Name == nil && ch.SN == nil && ch.Type == nil && ch.Status == nil &&
		ch.Note == nil && ch.SentDate == nil && ch.ReturnDate == nil &&
		!ch.ClearSent && !ch.ClearReturn
}

// toChanges translates a request body into the internal patch. With replace set
// (PUT) every omitted field is reset; otherwise (PATCH) omitted fields are kept.
func (b itemWriteBody) toChanges(replace bool) itemChanges {
	empty := ""
	ch := itemChanges{PersonnelID: b.PersonnelID}
	if replace {
		ch.Name = &empty
		ch.SN = &empty
		ch.Type = &empty
		ch.Status = &empty
		ch.Note = &empty
		ch.ClearSent = true
		ch.ClearReturn = true
	}
	if b.Name != nil {
		ch.Name = b.Name
	}
	if b.SN != nil {
		ch.SN = b.SN
	}
	if b.Type != nil {
		ch.Type = b.Type
	}
	if b.Status != nil {
		ch.Status = b.Status
	}
	if b.Note != nil {
		ch.Note = b.Note
	}
	if b.SentDate != nil {
		ch.SentDate = b.SentDate
		ch.ClearSent = *b.SentDate == ""
	}
	if b.ReturnDate != nil {
		ch.ReturnDate = b.ReturnDate
		ch.ClearReturn = *b.ReturnDate == ""
	}
	return ch
}

// ---------------------------------------------------------------------------
// SQL building blocks
// ---------------------------------------------------------------------------

// itemSelect lists every column an Item needs, with the joins that resolve the
// type/status labels (all three locales; the request locale is chosen in Go) and
// the usernames of the creator and last editor.
const itemSelect = `SELECT
	i.id, i.asset_tag, i.name, i.sn,
	t.value, t.label_en, t.label_cs, t.label_it,
	s.value, s.label_en, s.label_cs, s.label_it, s.color,
	i.sent_date, i.return_date, i.note, i.version, i.archived,
	i.label_printed_at, i.created_at, i.updated_at,
	cu.username, uu.username, i.personnel_id, p.name
FROM items i
LEFT JOIN item_types t ON t.id = i.type_id
LEFT JOIN statuses   s ON s.id = i.status_id
LEFT JOIN users     cu ON cu.id = i.created_by
LEFT JOIN users     uu ON uu.id = i.updated_by
LEFT JOIN personnel p ON p.id = i.personnel_id`

// itemStateSelect loads the writable state of one row.
const itemStateSelect = `SELECT
	i.id, i.asset_tag, i.name, i.sn, i.type_id, i.status_id,
	t.value, s.value, i.sent_date, i.return_date, i.note, i.version, i.archived, i.personnel_id
FROM items i
LEFT JOIN item_types t ON t.id = i.type_id
LEFT JOIN statuses   s ON s.id = i.status_id`

// itemSortColumns maps the public sort keys to SQL expressions. The values are
// a closed set, so user input never reaches the query text.
var itemSortColumns = map[string]string{
	"asset_tag":   "i.asset_tag",
	"name":        "lower(i.name)",
	"sn":          "lower(i.sn)",
	"type":        "t.value",
	"status":      "s.value",
	"sent_date":   "i.sent_date",
	"return_date": "i.return_date",
	"note":        "lower(i.note)",
	"updated_at":  "i.updated_at",
}

// queryer is satisfied by *sql.DB and *sql.Tx, so read helpers can run both
// standalone and inside a write transaction.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// rowScanner is satisfied by *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// itemFilter is a parsed item list query.
type itemFilter struct {
	Q        string
	Type     string
	Status   string
	Archived bool
	Overdue  bool
	Sort     string
	Order    string
	Limit    int
	Offset   int
	Locale   string
	Today    string
}

// where renders the filter as a SQL WHERE clause plus its arguments.
func (f itemFilter) where() (string, []any) {
	clauses := []string{"i.archived = ?"}
	args := []any{boolToInt(f.Archived)}

	if f.Q != "" {
		like := "%" + strings.ToLower(f.Q) + "%"
		clauses = append(clauses,
			`(lower(i.asset_tag) LIKE ? OR lower(i.name) LIKE ? OR lower(i.sn) LIKE ? OR lower(i.note) LIKE ?)`)
		args = append(args, like, like, like, like)
	}
	if f.Type != "" {
		clauses = append(clauses, "t.value = ?")
		args = append(args, f.Type)
	}
	if f.Status != "" {
		clauses = append(clauses, "s.value = ?")
		args = append(args, f.Status)
	}
	if f.Overdue {
		clauses = append(clauses, "i.return_date IS NOT NULL AND i.return_date <> '' AND i.return_date <= ? AND lower(COALESCE(s.value, '')) <> 'feasibility'")
		args = append(args, f.Today)
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// orderBy renders the ORDER BY clause. Nullable columns sort last in both
// directions so that pagination stays stable when a filter is applied.
func (f itemFilter) orderBy() string {
	column, ok := itemSortColumns[f.Sort]
	if !ok {
		column = itemSortColumns["asset_tag"]
	}
	direction := "ASC"
	if f.Order == sortDesc {
		direction = "DESC"
	}
	nullsLast := ""
	switch f.Sort {
	case "type", "status", "sent_date", "return_date":
		nullsLast = "(" + column + " IS NULL) ASC, "
	}
	return " ORDER BY " + nullsLast + column + " " + direction + ", i.id ASC"
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// scanItem reads one Item row. Labels are resolved for locale; when a type or
// status is set, label values fall back to English and finally to the machine
// key so the SPA never has to render an empty badge.
func scanItem(sc rowScanner, locale string) (Item, error) {
	var (
		item       Item
		archived   int
		typeValue  sql.NullString
		typeEn     sql.NullString
		typeCs     sql.NullString
		typeIt     sql.NullString
		statusVal  sql.NullString
		statusEn   sql.NullString
		statusCs   sql.NullString
		statusIt   sql.NullString
		statusCol  sql.NullString
		sentDate   sql.NullString
		returnDate sql.NullString
		printed    sql.NullString
		createdBy  sql.NullString
		updatedBy  sql.NullString
	)
	if err := sc.Scan(
		&item.ID, &item.AssetTag, &item.Name, &item.SN,
		&typeValue, &typeEn, &typeCs, &typeIt,
		&statusVal, &statusEn, &statusCs, &statusIt, &statusCol,
		&sentDate, &returnDate, &item.Note, &item.Version, &archived,
		&printed, &item.CreatedAt, &item.UpdatedAt,
		&createdBy, &updatedBy, &item.PersonnelID, &item.PersonnelName,
	); err != nil {
		return Item{}, err
	}

	item.Archived = archived != 0
	item.Type = nullString(typeValue)
	item.Status = nullString(statusVal)
	item.StatusColor = nullString(statusCol)
	// Business dates and the label timestamp render as null when unset; a legacy
	// empty string is treated as unset rather than as an empty value.
	item.SentDate = nullStringValue(sentDate)
	item.ReturnDate = nullStringValue(returnDate)
	item.LabelPrintedAt = nullStringValue(printed)
	item.CreatedBy = nullString(createdBy)
	item.UpdatedBy = nullString(updatedBy)

	if item.Type != nil {
		label := pickLabel(locale, typeEn.String, typeCs.String, typeIt.String, *item.Type)
		item.TypeLabel = &label
	}
	if item.Status != nil {
		label := pickLabel(locale, statusEn.String, statusCs.String, statusIt.String, *item.Status)
		if fixed := statusnames.Fixed(*item.Status); fixed != "" {
			label = fixed
		}
		item.StatusLabel = &label
	}
	return item, nil
}

// queryItems runs a filtered, sorted, paginated list query and returns the page
// plus the total number of matching rows.
func (a *API) queryItems(ctx context.Context, f itemFilter) ([]Item, int, error) {
	where, args := f.where()

	var total int
	if err := a.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM items i
		 LEFT JOIN item_types t ON t.id = i.type_id
		 LEFT JOIN statuses   s ON s.id = i.status_id`+where,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("api: count items: %w", err)
	}

	query := itemSelect + where + f.orderBy() + " LIMIT ? OFFSET ?"
	pageArgs := append(append(make([]any, 0, len(args)+2), args...), f.Limit, f.Offset)

	rows, err := a.db.QueryContext(ctx, query, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("api: list items: %w", err)
	}
	defer rows.Close()

	items := make([]Item, 0, f.Limit)
	for rows.Next() {
		item, err := scanItem(rows, f.Locale)
		if err != nil {
			return nil, 0, fmt.Errorf("api: scan item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("api: iterate items: %w", err)
	}
	return items, total, nil
}

// getItemByID loads one item by primary key.
func (a *API) getItemByID(ctx context.Context, q queryer, id int64, locale string) (Item, error) {
	row := q.QueryRowContext(ctx, itemSelect+" WHERE i.id = ?", id)
	item, err := scanItem(row, locale)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, errItemNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("api: load item %d: %w", id, err)
	}
	return item, nil
}

// getItemByTag loads one item by asset tag (case-insensitive, as the barcode
// contract requires).
func (a *API) getItemByTag(ctx context.Context, q queryer, tag, locale string) (Item, error) {
	row := q.QueryRowContext(ctx, itemSelect+" WHERE i.asset_tag = ? COLLATE NOCASE", tag)
	item, err := scanItem(row, locale)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, errItemNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("api: load item %q: %w", tag, err)
	}
	return item, nil
}

// loadItemState loads the writable state of one row, including the type/status
// machine keys used for the audit snapshot.
func (a *API) loadItemState(ctx context.Context, q queryer, id int64) (itemState, error) {
	var (
		st        itemState
		archived  int
		typeValue sql.NullString
		statusVal sql.NullString
	)
	err := q.QueryRowContext(ctx, itemStateSelect+" WHERE i.id = ?", id).Scan(
		&st.ID, &st.AssetTag, &st.Name, &st.SN, &st.TypeID, &st.StatusID,
		&typeValue, &statusVal, &st.SentDate, &st.ReturnDate, &st.Note, &st.Version, &archived, &st.PersonnelID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return itemState{}, errItemNotFound
	}
	if err != nil {
		return itemState{}, fmt.Errorf("api: load item state %d: %w", id, err)
	}
	st.Archived = archived != 0
	st.Type = nullString(typeValue)
	st.Status = nullString(statusVal)
	return st, nil
}

// nextAssetTag returns the next free PV-### tag inside the caller's write
// transaction. It takes the highest numeric suffix in use and adds one, so
// existing tags (including legacy gaps) are never reused or resequenced.
func nextAssetTag(ctx context.Context, q queryer) (string, error) {
	var highest int64
	err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(CAST(substr(asset_tag, 4) AS INTEGER)), 0)
		FROM items
		WHERE upper(substr(asset_tag, 1, 3)) = 'PV-' AND substr(asset_tag, 4) GLOB '[0-9]*'`).Scan(&highest)
	if err != nil {
		return "", fmt.Errorf("api: next asset tag: %w", err)
	}
	return fmt.Sprintf("PV-%03d", highest+1), nil
}

// assetTagExists reports whether a tag is taken, regardless of archival state:
// an archived label is still attached to the physical item.
func assetTagExists(ctx context.Context, q queryer, tag string) (bool, error) {
	var count int
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM items WHERE asset_tag = ? COLLATE NOCASE`, tag).Scan(&count); err != nil {
		return false, fmt.Errorf("api: check asset tag: %w", err)
	}
	return count > 0, nil
}

// ---------------------------------------------------------------------------
// Writes
// ---------------------------------------------------------------------------

// resolveTypeID maps a type machine key to its id. An empty key clears the
// reference; an unknown key is reported as errUnknownType.
func resolveTypeID(ctx context.Context, q queryer, value string) (sql.NullInt64, error) {
	if strings.TrimSpace(value) == "" {
		return sql.NullInt64{}, nil
	}
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM item_types WHERE value = ? COLLATE NOCASE`, value).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.NullInt64{}, fmt.Errorf("%w: %s", errUnknownType, value)
	}
	if err != nil {
		return sql.NullInt64{}, fmt.Errorf("api: resolve type: %w", err)
	}
	return sql.NullInt64{Int64: id, Valid: true}, nil
}

// resolveStatusID maps a status machine key to its id.
func resolveStatusID(ctx context.Context, q queryer, value string) (sql.NullInt64, error) {
	if strings.TrimSpace(value) == "" {
		return sql.NullInt64{}, nil
	}
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM statuses WHERE value = ? COLLATE NOCASE`, value).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.NullInt64{}, fmt.Errorf("%w: %s", errUnknownStatus, value)
	}
	if err != nil {
		return sql.NullInt64{}, fmt.Errorf("api: resolve status: %w", err)
	}
	return sql.NullInt64{Int64: id, Valid: true}, nil
}

// assetTagProblem reports why an explicitly supplied tag is unusable, or "" when
// it is acceptable. The barcode contract encodes the tag in CODE128 Code Set B
// (printable ASCII) and scans it back, so control characters and embedded
// whitespace are rejected: they would produce a label that cannot be resolved.
func assetTagProblem(tag string) string {
	if tag == "" {
		return "asset_tag must not be empty when provided"
	}
	if len(tag) > maxAssetTagLen {
		return fmt.Sprintf("asset_tag must not exceed %d characters", maxAssetTagLen)
	}
	for _, char := range tag {
		if char < 0x21 || char > 0x7e {
			return "asset_tag must be printable ASCII without spaces"
		}
	}
	return ""
}

// itemTarget is the resolved row content produced by applying itemChanges.
type itemTarget struct {
	PersonnelID sql.NullInt64
	Name        string
	SN          string
	TypeID      sql.NullInt64
	StatusID    sql.NullInt64
	SentDate    sql.NullString
	ReturnDate  sql.NullString
	Note        string
}

// apply builds the target row by applying ch on top of st, resolving type and
// status keys to ids. It returns errUnknownType/errUnknownStatus or
// errInvalidDate for bad input.
func (a *API) apply(ctx context.Context, q queryer, st itemState, ch itemChanges) (itemTarget, error) {
	target := itemTarget{
		PersonnelID: st.PersonnelID,
		Name:        st.Name,
		SN:          st.SN,
		TypeID:      st.TypeID,
		StatusID:    st.StatusID,
		SentDate:    st.SentDate,
		ReturnDate:  st.ReturnDate,
		Note:        st.Note,
	}

	if ch.PersonnelID != nil {
		target.PersonnelID = sql.NullInt64{Int64: *ch.PersonnelID, Valid: *ch.PersonnelID > 0}
	}
	if ch.Name != nil {
		target.Name = strings.TrimSpace(*ch.Name)
	}
	if ch.SN != nil {
		target.SN = strings.TrimSpace(*ch.SN)
	}
	if ch.Note != nil {
		target.Note = strings.TrimSpace(*ch.Note)
	}
	if ch.Type != nil {
		id, err := resolveTypeID(ctx, q, *ch.Type)
		if err != nil {
			return itemTarget{}, err
		}
		target.TypeID = id
	}
	if ch.Status != nil {
		id, err := resolveStatusID(ctx, q, *ch.Status)
		if err != nil {
			return itemTarget{}, err
		}
		target.StatusID = id
	}

	switch {
	case ch.ClearSent:
		target.SentDate = sql.NullString{}
	case ch.SentDate != nil:
		value, err := normalizeDate(*ch.SentDate)
		if err != nil {
			return itemTarget{}, &validationError{details: []string{"sent_date " + err.Error()}}
		}
		target.SentDate = value
	}
	switch {
	case ch.ClearReturn:
		target.ReturnDate = sql.NullString{}
	case ch.ReturnDate != nil:
		value, err := normalizeDate(*ch.ReturnDate)
		if err != nil {
			return itemTarget{}, &validationError{details: []string{"return_date " + err.Error()}}
		}
		target.ReturnDate = value
	}
	return target, nil
}

// validateTarget applies the field-length limits shared by every write path.
func validateTarget(t itemTarget) []string {
	var details []string
	if len(t.Name) > maxNameLen {
		details = append(details, fmt.Sprintf("name must not exceed %d characters", maxNameLen))
	}
	if len(t.SN) > maxSNLen {
		details = append(details, fmt.Sprintf("sn must not exceed %d characters", maxSNLen))
	}
	if len(t.Note) > maxNoteLen {
		details = append(details, fmt.Sprintf("note must not exceed %d characters", maxNoteLen))
	}
	return details
}

// updateItem writes target to the row, bumping the optimistic-concurrency
// version, and returns the reloaded item. The version predicate is a second
// guard behind the caller's check: no caller may update a row it did not read.
func (a *API) updateItem(ctx context.Context, tx *sql.Tx, st itemState, target itemTarget, actor *auth.User, locale string) (Item, error) {
	now := a.now().UTC().Format(time.RFC3339)
	res, err := tx.ExecContext(ctx, `UPDATE items SET
			name = ?, sn = ?, type_id = ?, status_id = ?, sent_date = ?, return_date = ?, note = ?, personnel_id = ?,
			version = version + 1, updated_at = ?, updated_by = ?
		WHERE id = ? AND version = ?`,
		target.Name, target.SN, target.TypeID, target.StatusID, target.SentDate, target.ReturnDate, target.Note, target.PersonnelID,
		now, nullID(actor), st.ID, st.Version,
	)
	if err != nil {
		return Item{}, fmt.Errorf("api: update item %d: %w", st.ID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Item{}, fmt.Errorf("api: update item %d: %w", st.ID, err)
	}
	if affected == 0 {
		// The row is locked for this transaction, so this can only happen if the
		// caller passed a stale state.
		return Item{}, errVersionConflict
	}
	return a.getItemByID(ctx, tx, st.ID, locale)
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// mountItemRoutes registers the item endpoints on the protected router.
// Mutations require the editor role; reading and scan resolution are available
// to viewers too.
func (a *API) mountItemRoutes(r chi.Router) {
	editor := a.auth.RequireRole(auth.RoleAdmin)

	r.With(editor).Post("/items", a.handleCreateItem)
	r.With(editor).Put("/items/{itemId}", a.handleUpdateItem)
	r.With(editor).Delete("/items/{itemId}", a.handleDeleteItem)
}

// handleListItems implements GET /api/v1/items.
func (a *API) handleListItems(w http.ResponseWriter, r *http.Request) {
	filter, err := a.parseItemFilter(r)
	if err != nil {
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, err.Error())
		return
	}

	if filter.Archived && auth.UserFrom(r.Context()) == nil {
		Error(w, r, http.StatusForbidden, CodeForbidden, "archived equipment is staff-only")
		return
	}
	items, total, err := a.queryItems(r.Context(), filter)
	if err != nil {
		InternalError(w, r, a.log, err)
		return
	}

	totalPages := 0
	if total > 0 {
		totalPages = (total + filter.Limit - 1) / filter.Limit
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"data": items,
		"meta": itemListMeta{
			Page:       filter.Offset/filter.Limit + 1,
			PerPage:    filter.Limit,
			Total:      total,
			TotalPages: totalPages,
		},
	})
}

// handleGetItem implements GET /api/v1/items/{itemId}.
func (a *API) handleGetItem(w http.ResponseWriter, r *http.Request) {
	id, err := itemIDFromRequest(r)
	if err != nil {
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, err.Error())
		return
	}
	item, err := a.getItemByID(r.Context(), a.db, id, requestLocale(r))
	if errors.Is(err, errItemNotFound) {
		Error(w, r, http.StatusNotFound, CodeNotFound, "item not found")
		return
	}
	if err != nil {
		InternalError(w, r, a.log, err)
		return
	}
	if item.Archived && auth.UserFrom(r.Context()) == nil {
		Error(w, r, http.StatusNotFound, CodeNotFound, "item not found")
		return
	}
	writeData(w, http.StatusOK, item)
}

// handleCreateItem implements POST /api/v1/items. The asset tag is optional: an
// omitted tag is allocated as the next free PV-### inside the same transaction.
func (a *API) handleCreateItem(w http.ResponseWriter, r *http.Request) {
	var body itemWriteBody
	if err := DecodeJSON(w, r, &body); err != nil {
		DecodeError(w, r, err)
		return
	}
	if body.Version != nil {
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, "version must not be set when creating an item")
		return
	}

	if body.Status == nil {
		value := "Feasibility"
		body.Status = &value
	}
	locale := requestLocale(r)
	actor := auth.UserFrom(r.Context())
	var created Item

	err := a.withTx(r.Context(), func(tx *sql.Tx) error {
		tag := ""
		if body.AssetTag != nil {
			tag = strings.TrimSpace(*body.AssetTag)
			if problem := assetTagProblem(tag); problem != "" {
				return &validationError{details: []string{problem}}
			}
			exists, err := assetTagExists(r.Context(), tx, tag)
			if err != nil {
				return err
			}
			if exists {
				return &duplicateError{entity: "asset_tag", value: tag}
			}
		} else {
			auto, err := nextAssetTag(r.Context(), tx)
			if err != nil {
				return err
			}
			tag = auto
		}

		target, err := a.apply(r.Context(), tx, itemState{}, body.toChanges(false))
		if err != nil {
			return err
		}
		if err := a.validateRental(r, tx, itemState{}, target, body.toChanges(false)); err != nil {
			return err
		}
		if details := validateTarget(target); len(details) > 0 {
			return &validationError{details: details}
		}

		now := a.now().UTC().Format(time.RFC3339)
		res, err := tx.ExecContext(r.Context(), `INSERT INTO items
			(asset_tag, name, sn, type_id, status_id, sent_date, return_date, note, personnel_id,
			 version, archived, created_at, updated_at, created_by, updated_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 0, ?, ?, ?, ?)`,
			tag, target.Name, target.SN, target.TypeID, target.StatusID,
			target.SentDate, target.ReturnDate, target.Note, target.PersonnelID,
			now, now, nullID(actor), nullID(actor),
		)
		if err != nil {
			return fmt.Errorf("api: insert item: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("api: insert item: %w", err)
		}
		created, err = a.getItemByID(r.Context(), tx, id, locale)
		if err != nil {
			return err
		}
		return a.recordAudit(r.Context(), tx, a.auditRecord(r, audit.ActionCreate, audit.EntityItem, created.AssetTag, nil, created.snapshot()))
	})
	if err != nil {
		a.writeItemError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, created)
}

// handleUpdateItem implements PUT /api/v1/items/{itemId}: the body is the new
// mutable state, so fields it omits are reset.
func (a *API) handleUpdateItem(w http.ResponseWriter, r *http.Request) {
	a.writeItem(w, r, true)
}

// handlePatchItem implements PATCH /api/v1/items/{itemId}: only the fields
// present in the body change.
func (a *API) handlePatchItem(w http.ResponseWriter, r *http.Request) {
	a.writeItem(w, r, false)
}

// writeItem is the shared PUT/PATCH implementation.
func (a *API) writeItem(w http.ResponseWriter, r *http.Request, replace bool) {
	id, err := itemIDFromRequest(r)
	if err != nil {
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, err.Error())
		return
	}
	var body itemWriteBody
	if err := DecodeJSON(w, r, &body); err != nil {
		DecodeError(w, r, err)
		return
	}
	if body.Version == nil {
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, "version is required")
		return
	}
	if *body.Version < 1 {
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, "version must be a positive integer")
		return
	}
	if !isAdmin(r) && (replace || body.AssetTag != nil || body.Name != nil || body.SN != nil || body.Type != nil) {
		Error(w, r, http.StatusForbidden, CodeForbidden, "public Editor may only update rental fields")
		return
	}
	changes := body.toChanges(replace)
	// Feasibility locks historical rental details, including full replacement.
	if replace && body.Status != nil && strings.EqualFold(*body.Status, "Feasibility") {
		changes.Note = body.Note
		changes.SentDate = body.SentDate
		changes.ReturnDate = body.ReturnDate
		changes.ClearSent = body.SentDate != nil && *body.SentDate == ""
		changes.ClearReturn = body.ReturnDate != nil && *body.ReturnDate == ""
	}
	if changes.empty() {
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, "request does not change any field")
		return
	}

	locale := requestLocale(r)
	var updated Item

	err = a.withTx(r.Context(), func(tx *sql.Tx) error {
		st, err := a.loadItemState(r.Context(), tx, id)
		if err != nil {
			return err
		}
		if st.Version != *body.Version {
			return &versionConflictError{entity: "item", ref: st.AssetTag, expected: *body.Version, current: st.Version}
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
		updated, err = a.updateItem(r.Context(), tx, st, target, auth.UserFrom(r.Context()), locale)
		if err != nil {
			return err
		}
		return a.recordAudit(r.Context(), tx, a.auditRecord(r, audit.ActionUpdate, audit.EntityItem, updated.AssetTag, st.snapshot(), updated.snapshot()))
	})
	if err != nil {
		a.writeItemError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, updated)
}

// handleDeleteItem implements DELETE /api/v1/items/{itemId}: a soft delete that
// keeps the row (and its audit history) and bumps the version so any in-flight
// edit with a stale version fails instead of resurrecting the item.
func (a *API) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	id, err := itemIDFromRequest(r)
	if err != nil {
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, err.Error())
		return
	}
	locale := requestLocale(r)

	err = a.withTx(r.Context(), func(tx *sql.Tx) error {
		st, err := a.loadItemState(r.Context(), tx, id)
		if err != nil {
			return err
		}
		if st.Archived {
			// Already archived: report success without touching the row so the
			// audit log stays free of no-op entries.
			return nil
		}
		before := st.snapshot()
		now := a.now().UTC().Format(time.RFC3339)
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE items SET archived = 1, version = version + 1, updated_at = ?, updated_by = ? WHERE id = ? AND version = ?`,
			now, nullID(auth.UserFrom(r.Context())), st.ID, st.Version,
		); err != nil {
			return fmt.Errorf("api: archive item %d: %w", st.ID, err)
		}
		after, err := a.getItemByID(r.Context(), tx, st.ID, locale)
		if err != nil {
			return err
		}
		return a.recordAudit(r.Context(), tx, a.auditRecord(r, audit.ActionDelete, audit.EntityItem, after.AssetTag, before, after.snapshot()))
	})
	if err != nil {
		a.writeItemError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"ok": true})
}

// handleResolveItems implements POST /api/v1/items/resolve: it normalizes the
// scanned CODE128 payloads, matches them against asset tags and reports the
// codes that resolved to nothing so the UI can offer to create a draft.
func (a *API) handleResolveItems(w http.ResponseWriter, r *http.Request) {
	var body resolveRequest
	if err := DecodeJSON(w, r, &body); err != nil {
		DecodeError(w, r, err)
		return
	}
	if len(body.Codes) == 0 {
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, "codes must not be empty")
		return
	}
	if len(body.Codes) > maxResolveCodes {
		Error(w, r, http.StatusBadRequest, CodeValidationFailed,
			fmt.Sprintf("at most %d codes may be resolved per request", maxResolveCodes))
		return
	}

	codes := normalizeCodes(body.Codes)
	locale := requestLocale(r)

	matched := make([]Item, 0, len(codes))
	unknown := make([]string, 0, len(codes))
	results := make(map[string]string, len(codes)) // code -> matched|unknown
	byTag := make(map[string]Item, len(codes))

	// One query for the whole batch; codes are normalized and capped above.
	args := make([]any, 0, len(codes))
	placeholders := make([]string, 0, len(codes))
	for _, code := range codes {
		placeholders = append(placeholders, "?")
		args = append(args, code)
	}
	rows, err := a.db.QueryContext(r.Context(),
		itemSelect+" WHERE upper(i.asset_tag) IN ("+strings.Join(placeholders, ", ")+")", args...)
	if err != nil {
		InternalError(w, r, a.log, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanItem(rows, locale)
		if err != nil {
			InternalError(w, r, a.log, fmt.Errorf("api: scan resolved item: %w", err))
			return
		}
		if item.Archived && auth.UserFrom(r.Context()) == nil {
			continue
		}
		byTag[strings.ToUpper(item.AssetTag)] = item
	}
	if err := rows.Err(); err != nil {
		InternalError(w, r, a.log, err)
		return
	}

	// Preserve scan order: the client appends to a buffer in the order the
	// labels were read.
	for _, code := range codes {
		if item, ok := byTag[code]; ok {
			matched = append(matched, item)
			results[code] = scanResultMatched
			continue
		}
		unknown = append(unknown, code)
		results[code] = scanResultUnknown
	}

	a.recordScanEvents(r, codes, byTag, results)

	WriteJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"matched": matched, "unknown": unknown},
	})
}

// ---------------------------------------------------------------------------
// Shared helpers (used by options.go, export.go and bulk.go)
// ---------------------------------------------------------------------------

// ErrItemNotFound is returned by the read helpers when a row does not exist.
var errItemNotFound = errors.New("item not found")

// ErrVersionConflict is returned when the caller's version is stale.
var errVersionConflict = errors.New("version conflict")

// errUnknownType and errUnknownStatus report references to option values that
// are not configured.
var errUnknownType = errors.New("unknown item type")
var errUnknownStatus = errors.New("unknown status")

// validationError carries one or more human-readable validation details.
type validationError struct {
	details []string
}

func (e *validationError) Error() string {
	return "validation failed: " + strings.Join(e.details, "; ")
}

// duplicateError reports a unique-key collision.
type duplicateError struct {
	entity string
	value  string
}

func (e *duplicateError) Error() string {
	return fmt.Sprintf("%s %q already exists", e.entity, e.value)
}

// versionConflictError reports a stale optimistic-concurrency version.
type versionConflictError struct {
	entity   string
	ref      string
	expected int64
	current  int64
}

func (e *versionConflictError) Error() string {
	return fmt.Sprintf("%s %s: version conflict (expected %d, current %d)", e.entity, e.ref, e.expected, e.current)
}

// detail renders the conflict as an error-envelope detail string.
func (e *versionConflictError) detail() string {
	return fmt.Sprintf("%s: expected version %d, current version %d", e.ref, e.expected, e.current)
}

// writeItemError maps the domain errors above onto the frozen error envelope.
func (a *API) writeItemError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *validationError
	var duplicate *duplicateError
	var conflict *versionConflictError

	switch {
	case errors.Is(err, errItemNotFound):
		Error(w, r, http.StatusNotFound, CodeNotFound, "item not found")
	case errors.As(err, &conflict):
		// schema.md: 409 with code version_conflict and the current version.
		Error(w, r, http.StatusConflict, CodeConflict, "the item was changed by someone else", conflict.detail())
	case errors.Is(err, errVersionConflict):
		// Defensive: the write was rejected by its own version predicate.
		Error(w, r, http.StatusConflict, CodeConflict, "the item was changed by someone else")
	case errors.As(err, &duplicate):
		Error(w, r, http.StatusConflict, codeConflict, "duplicate value", err.Error())
	case errors.As(err, &validation):
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, validation.Error(), validation.details...)
	case errors.Is(err, errUnknownType), errors.Is(err, errUnknownStatus):
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, "unknown option", err.Error())
	default:
		InternalError(w, r, a.log, err)
	}
}

// parseItemID reads the {itemId} path parameter.
func itemIDFromRequest(r *http.Request) (int64, error) {
	raw := chi.URLParam(r, "itemId")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("itemId must be a positive integer")
	}
	return id, nil
}

// parseItemFilter turns the query string into an itemFilter, validating the
// enumerations the contract declares.
func (a *API) parseItemFilter(r *http.Request) (itemFilter, error) {
	query := r.URL.Query()

	filter := itemFilter{
		Q:      strings.TrimSpace(query.Get("q")),
		Type:   strings.TrimSpace(query.Get("type")),
		Status: strings.TrimSpace(query.Get("status")),
		Locale: requestLocale(r),
		Today:  a.today(),
	}

	archived, err := boolParam(query.Get("archived"), false)
	if err != nil {
		return itemFilter{}, fmt.Errorf("archived: %w", err)
	}
	filter.Archived = archived

	overdue, err := boolParam(query.Get("overdue"), false)
	if err != nil {
		return itemFilter{}, fmt.Errorf("overdue: %w", err)
	}
	filter.Overdue = overdue

	sortKey := query.Get("sort")
	if sortKey == "" {
		sortKey = "asset_tag"
	}
	if _, ok := itemSortColumns[sortKey]; !ok {
		return itemFilter{}, fmt.Errorf("sort must be one of %s", strings.Join(itemSortKeys(), ", "))
	}
	filter.Sort = sortKey

	order := strings.ToLower(query.Get("order"))
	if order == "" {
		order = sortAsc
	}
	if order != sortAsc && order != sortDesc {
		return itemFilter{}, fmt.Errorf("order must be %s or %s", sortAsc, sortDesc)
	}
	filter.Order = order

	page, err := intParam(query.Get("page"), 1, 1, maxPage)
	if err != nil {
		return itemFilter{}, fmt.Errorf("page: %w", err)
	}
	perPage, err := intParam(query.Get("per_page"), defaultPerPage, 1, maxPerPage)
	if err != nil {
		return itemFilter{}, fmt.Errorf("per_page: %w", err)
	}
	filter.Limit = perPage
	filter.Offset = (page - 1) * perPage
	return filter, nil
}

// Sort direction values (the contract's `order` enum).
const (
	sortAsc  = "asc"
	sortDesc = "desc"
)

// Scan result values recorded in scan_events.
const (
	scanResultMatched = "matched"
	scanResultUnknown = "unknown"
)

// itemSortKeys returns the accepted sort keys in a stable order for messages.
func itemSortKeys() []string {
	return []string{"asset_tag", "name", "sn", "type", "status", "sent_date", "return_date", "note", "updated_at"}
}

// withTx runs fn inside one IMMEDIATE transaction. The store configures
// _txlock=immediate, so the write lock is held for the whole callback and the
// read-then-write sequences in this package cannot interleave with another
// writer.
func (a *API) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("api: begin transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("api: commit transaction: %w", err)
	}
	return nil
}

// auditRecord builds an audit event for the authenticated actor behind r.
func (a *API) auditRecord(r *http.Request, action, entityType, entityID string, before, after any) audit.Record {
	return audit.Record{
		Action:      action,
		EntityType:  entityType,
		EntityID:    entityID,
		ActorUserID: actorID(auth.UserFrom(r.Context())),
		Before:      before,
		After:       after,
		IP:          ClientIP(r, a.cfg.TrustProxy),
		UserAgent:   r.UserAgent(),
	}
}

// recordAudit appends one audit row inside the caller's transaction. It is a
// no-op when no writer is configured, so handlers never nil-check.
func (a *API) recordAudit(ctx context.Context, tx *sql.Tx, rec audit.Record) error {
	if a.audit == nil {
		return nil
	}
	return a.audit.RecordTx(ctx, tx, rec)
}

// snapshot renders the mutable state of an item for the audit log.
func (st itemState) snapshot() itemSnapshot {
	return itemSnapshot{
		PersonnelID: nullableInt(st.PersonnelID),
		AssetTag:    st.AssetTag,
		Name:        st.Name,
		SN:          st.SN,
		Type:        st.Type,
		Status:      st.Status,
		SentDate:    nullStringValue(st.SentDate),
		ReturnDate:  nullStringValue(st.ReturnDate),
		Note:        st.Note,
		Version:     st.Version,
		Archived:    st.Archived,
	}
}

// snapshot renders an item for the audit log.
func (it Item) snapshot() itemSnapshot {
	return itemSnapshot{
		PersonnelID: it.PersonnelID,
		AssetTag:    it.AssetTag,
		Name:        it.Name,
		SN:          it.SN,
		Type:        it.Type,
		Status:      it.Status,
		SentDate:    it.SentDate,
		ReturnDate:  it.ReturnDate,
		Note:        it.Note,
		Version:     it.Version,
		Archived:    it.Archived,
	}
}

// recordScanEvents appends one scan_events row per unique scanned code. Scan
// telemetry must never break scanning, so failures are logged and ignored.
func (a *API) recordScanEvents(r *http.Request, codes []string, byTag map[string]Item, results map[string]string) {
	if a.db == nil || len(codes) == 0 {
		return
	}
	actor := actorID(auth.UserFrom(r.Context()))
	now := a.now().UTC().Format(time.RFC3339)

	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		a.log.Warn("failed to record scan events", "error", err)
		return
	}
	for _, code := range codes {
		var itemID any
		if item, ok := byTag[code]; ok {
			itemID = item.ID
		}
		if _, err := tx.ExecContext(r.Context(),
			`INSERT INTO scan_events (actor_user_id, asset_tag, item_id, result, created_at) VALUES (?, ?, ?, ?, ?)`,
			actor, code, itemID, results[code], now,
		); err != nil {
			_ = tx.Rollback()
			a.log.Warn("failed to record scan event", "error", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		a.log.Warn("failed to commit scan events", "error", err)
	}
}

// normalizeCodes trims, collapses internal whitespace and upper-cases each
// scanned payload, then removes duplicates while preserving scan order — the
// barcode contract's normalization rules.
func normalizeCodes(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, code := range raw {
		normalized := normalizeCode(code)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

// normalizeCode applies the barcode contract's normalization to one payload.
func normalizeCode(raw string) string {
	return strings.ToUpper(strings.Join(strings.Fields(raw), " "))
}

// normalizeDate validates an ISO business date (YYYY-MM-DD) and returns it as a
// nullable column value. An empty string clears the column.
func normalizeDate(value string) (sql.NullString, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return sql.NullString{}, nil
	}
	parsed, err := time.Parse(dateLayout, value)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("must be an ISO date (YYYY-MM-DD)")
	}
	return sql.NullString{String: parsed.Format(dateLayout), Valid: true}, nil
}

// dateLayout is the business-date format from schema.md.
const dateLayout = "2006-01-02"

// today returns the server-local calendar date used by the overdue filter.
func (a *API) today() string {
	loc := a.cfg.Location
	if loc == nil {
		loc = time.UTC
	}
	return a.now().In(loc).Format(dateLayout)
}

// requestLocale resolves the label locale: ?lang=, then Accept-Language, then
// English. Unknown values are ignored rather than rejected, because content
// negotiation should never fail a request.
func requestLocale(r *http.Request) string {
	if lang := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang"))); isSupportedLocale(lang) {
		return lang
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag := strings.ToLower(strings.TrimSpace(part))
		if index := strings.IndexAny(tag, ";"); index >= 0 {
			tag = strings.TrimSpace(tag[:index])
		}
		if index := strings.IndexAny(tag, "-_"); index > 0 {
			tag = tag[:index]
		}
		if isSupportedLocale(tag) {
			return tag
		}
	}
	return localeDefault
}

// isSupportedLocale reports whether code is one of the app's languages.
func isSupportedLocale(code string) bool {
	switch code {
	case "en", "cs", "it":
		return true
	default:
		return false
	}
}

// localeDefault is the fallback label language (matches the settings default
// `locale.default`).
const localeDefault = "en"

// pickLabel selects a localized label, falling back to English and finally to
// the machine key so a badge is never rendered empty.
func pickLabel(locale, en, cs, it, fallback string) string {
	var chosen string
	switch locale {
	case "cs":
		chosen = cs
	case "it":
		chosen = it
	default:
		chosen = en
	}
	chosen = strings.TrimSpace(chosen)
	if chosen != "" {
		return chosen
	}
	if strings.TrimSpace(en) != "" {
		return en
	}
	return fallback
}

// writeData writes the standard success envelope {"data": v}.
func writeData(w http.ResponseWriter, status int, v any) {
	WriteJSON(w, status, map[string]any{"data": v})
}

// boolToInt renders a Go bool as the INTEGER the schema uses.
func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// nullString converts a nullable TEXT column into *string.
func nullString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	out := value.String
	return &out
}

// nullStringValue converts a nullable TEXT column into *string (nil when the
// value is absent or empty, which is how the API renders unset business dates).
func nullStringValue(value sql.NullString) *string {
	if !value.Valid || value.String == "" {
		return nil
	}
	out := value.String
	return &out
}

// nullID renders an actor's primary key as a nullable SQL value.
func nullID(user *auth.User) any {
	if user == nil {
		return nil
	}
	return user.ID
}

// actorID renders an actor's primary key as *int64 for the audit log.
func actorID(user *auth.User) *int64 {
	if user == nil {
		return nil
	}
	id := user.ID
	return &id
}

// boolParam parses an optional boolean query parameter. An empty value selects
// the default; a bare parameter (for example "?overdue") means true.
func boolParam(raw string, fallback bool) (bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback, nil
	}
	switch strings.ToLower(trimmed) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%q is not a boolean", raw)
	}
}

// intParam parses an optional integer query parameter with bounds.
func intParam(raw string, fallback, min, max int) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("%q is not an integer", raw)
	}
	if value < min {
		return 0, fmt.Errorf("must be at least %d", min)
	}
	if max > 0 && value > max {
		return 0, fmt.Errorf("must be at most %d", max)
	}
	return value, nil
}
