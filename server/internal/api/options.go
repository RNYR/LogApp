package api

// Item types and statuses: the localized, user-configurable option sets that
// items reference. PUT /options replaces both sets in one transaction and uses
// the optional `renames` map to rename a value in place, so items keep pointing
// at the same row instead of losing their type or status.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sklad/server/internal/statusnames"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"sklad/server/internal/audit"
	"sklad/server/internal/auth"
)

// Limits and defaults for option values.
const (
	maxOptionValueLen  = 64
	defaultStatusColor = "#93c47d" // statuses.color default (schema.md)
)

// hexColorPattern is the color format the SPA color picker produces.
var hexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ---------------------------------------------------------------------------
// Wire types (openapi TypeOption / StatusOption / OptionsUpdate)
// ---------------------------------------------------------------------------

// localizedLabels carries a label per supported language.
type localizedLabels struct {
	EN string `json:"en"`
	CS string `json:"cs"`
	IT string `json:"it"`
}

// typeOption is one item type as returned by the API.
type typeOption struct {
	Value     string          `json:"value"`
	Label     string          `json:"label"`
	Labels    localizedLabels `json:"labels"`
	SortOrder int             `json:"sort_order"`
}

// statusOption is one status as returned by the API.
type statusOption struct {
	Value     string          `json:"value"`
	Label     string          `json:"label"`
	Labels    localizedLabels `json:"labels"`
	Color     string          `json:"color"`
	SortOrder int             `json:"sort_order"`
}

// optionInput is one element of an OptionsUpdate request.
type optionInput struct {
	Value     string           `json:"value"`
	Label     string           `json:"label"`
	Labels    *localizedLabels `json:"labels"`
	Color     string           `json:"color"`
	SortOrder *int             `json:"sort_order"`
}

// optionsUpdateRequest is the body of PUT /api/v1/options.
type optionsUpdateRequest struct {
	Types    []optionInput `json:"types"`
	Statuses []optionInput `json:"statuses"`
	Renames  struct {
		Types    map[string]string `json:"types"`
		Statuses map[string]string `json:"statuses"`
	} `json:"renames"`
}

// optionRow is one row of item_types or statuses.
type optionRow struct {
	ID        int64
	Value     string
	Labels    localizedLabels
	Color     string
	SortOrder int
}

// optionsSnapshot is the audit representation of both option sets.
type optionsSnapshot struct {
	Types    []optionSnapshot `json:"types"`
	Statuses []optionSnapshot `json:"statuses"`
}

// optionSnapshot is one option in an audit snapshot.
type optionSnapshot struct {
	Value     string          `json:"value"`
	Labels    localizedLabels `json:"labels"`
	Color     string          `json:"color,omitempty"`
	SortOrder int             `json:"sort_order"`
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// loadOptions returns both option sets ordered for display.
func (a *API) loadOptions(ctx context.Context, q queryer) ([]optionRow, []optionRow, error) {
	types, err := loadOptionRows(ctx, q, `SELECT id, value, label_en, label_cs, label_it, sort_order
		FROM item_types ORDER BY sort_order, value COLLATE NOCASE`, false)
	if err != nil {
		return nil, nil, err
	}
	statuses, err := loadOptionRows(ctx, q, `SELECT id, value, label_en, label_cs, label_it, color, sort_order
		FROM statuses ORDER BY sort_order, value COLLATE NOCASE`, true)
	if err != nil {
		return nil, nil, err
	}
	return types, statuses, nil
}

// loadOptionRows scans an option query. The statuses query selects one extra
// column (color), selected by withColor.
func loadOptionRows(ctx context.Context, q queryer, query string, withColor bool) ([]optionRow, error) {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("api: load options: %w", err)
	}
	defer rows.Close()

	var out []optionRow
	for rows.Next() {
		var (
			row   optionRow
			label localizedLabels
		)
		dest := []any{&row.ID, &row.Value, &label.EN, &label.CS, &label.IT}
		if withColor {
			dest = append(dest, &row.Color)
		}
		dest = append(dest, &row.SortOrder)
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("api: scan option: %w", err)
		}
		row.Labels = label
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("api: iterate options: %w", err)
	}
	return out, nil
}

// optionsResponse renders both sets for the requested locale.
func optionsResponse(types, statuses []optionRow, locale string) map[string]any {
	typeList := make([]typeOption, 0, len(types))
	for _, row := range types {
		typeList = append(typeList, typeOption{
			Value:     row.Value,
			Label:     pickLabel(locale, row.Labels.EN, row.Labels.CS, row.Labels.IT, row.Value),
			Labels:    row.Labels,
			SortOrder: row.SortOrder,
		})
	}
	statusList := make([]statusOption, 0, len(statuses))
	for _, row := range statuses {
		if fixed := statusnames.Fixed(row.Value); fixed != "" {
			row.Labels = localizedLabels{EN: fixed, CS: fixed, IT: fixed}
		}
		color := row.Color
		if color == "" {
			color = defaultStatusColor
		}
		statusList = append(statusList, statusOption{
			Value:     row.Value,
			Label:     pickLabel(locale, row.Labels.EN, row.Labels.CS, row.Labels.IT, row.Value),
			Labels:    row.Labels,
			Color:     color,
			SortOrder: row.SortOrder,
		})
	}
	return map[string]any{"types": typeList, "statuses": statusList}
}

// optionSnapshotFrom renders an audit snapshot of both sets.
func optionSnapshotFrom(types, statuses []optionRow) optionsSnapshot {
	snapshot := optionsSnapshot{
		Types:    make([]optionSnapshot, 0, len(types)),
		Statuses: make([]optionSnapshot, 0, len(statuses)),
	}
	for _, row := range types {
		snapshot.Types = append(snapshot.Types, optionSnapshot{
			Value: row.Value, Labels: row.Labels, SortOrder: row.SortOrder,
		})
	}
	for _, row := range statuses {
		snapshot.Statuses = append(snapshot.Statuses, optionSnapshot{
			Value: row.Value, Labels: row.Labels, Color: row.Color, SortOrder: row.SortOrder,
		})
	}
	return snapshot
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// mountPublicOptionsRoutes registers GET /api/v1/options, which the contract
// leaves unauthenticated so the SPA can render before login.
func (a *API) mountPublicOptionsRoutes(r chi.Router) {
	r.Get("/options", a.handleListOptions)
}

// mountOptionsRoutes registers the protected options endpoints.
func (a *API) mountOptionsRoutes(r chi.Router) {
	r.With(a.auth.RequireRole(auth.RoleAdmin)).Put("/options", a.handlePutOptions)
}

// handleListOptions implements GET /api/v1/options.
func (a *API) handleListOptions(w http.ResponseWriter, r *http.Request) {
	types, statuses, err := a.loadOptions(r.Context(), a.db)
	if err != nil {
		InternalError(w, r, a.log, err)
		return
	}
	writeData(w, http.StatusOK, optionsResponse(types, statuses, requestLocale(r)))
}

// handlePutOptions implements PUT /api/v1/options: a full replace of both sets,
// with in-place renames and a refusal to delete options that items still use.
func (a *API) handlePutOptions(w http.ResponseWriter, r *http.Request) {
	var body optionsUpdateRequest
	if err := DecodeJSON(w, r, &body); err != nil {
		DecodeError(w, r, err)
		return
	}

	found := false
	for _, option := range body.Statuses {
		if option.Value == "Feasibility" {
			found = true
		}
	}
	for old, next := range body.Renames.Statuses {
		if strings.EqualFold(old, "Feasibility") || strings.EqualFold(next, "Feasibility") {
			Error(w, r, 400, CodeValidationFailed, "Feasibility cannot be renamed")
			return
		}
	}
	if !found {
		Error(w, r, 400, CodeValidationFailed, "Feasibility must remain available")
		return
	}
	types, err := normalizeOptionInputs(body.Types, false)
	if err != nil {
		a.writeOptionsError(w, r, err)
		return
	}
	statuses, err := normalizeOptionInputs(body.Statuses, true)
	if err != nil {
		a.writeOptionsError(w, r, err)
		return
	}

	locale := requestLocale(r)
	var response map[string]any

	err = a.withTx(r.Context(), func(tx *sql.Tx) error {
		existingTypes, existingStatuses, err := a.loadOptions(r.Context(), tx)
		if err != nil {
			return err
		}
		before := optionSnapshotFrom(existingTypes, existingStatuses)

		if err := a.applyOptionSet(r.Context(), tx, "item_types", existingTypes, types, body.Renames.Types, false); err != nil {
			return err
		}
		if err := a.applyOptionSet(r.Context(), tx, "statuses", existingStatuses, statuses, body.Renames.Statuses, true); err != nil {
			return err
		}

		afterTypes, afterStatuses, err := a.loadOptions(r.Context(), tx)
		if err != nil {
			return err
		}
		response = optionsResponse(afterTypes, afterStatuses, locale)

		return a.recordAudit(r.Context(), tx, a.auditRecord(r, audit.ActionOptionsUpdate, audit.EntityOptions, "options",
			before, optionSnapshotFrom(afterTypes, afterStatuses)))
	})
	if err != nil {
		a.writeOptionsError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, response)
}

// ---------------------------------------------------------------------------
// Replace algorithm
// ---------------------------------------------------------------------------

// normalizedOption is a validated incoming option.
type normalizedOption struct {
	Value     string
	Labels    localizedLabels
	Color     string
	SortOrder int
}

// normalizeOptionInputs validates one of the incoming sets: values must be
// non-empty, unique (case-insensitively) and labelled; sort_order is taken from
// the array position so the client controls the display order by reordering.
func normalizeOptionInputs(inputs []optionInput, withColor bool) ([]normalizedOption, error) {
	out := make([]normalizedOption, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))

	for index, in := range inputs {
		value := strings.TrimSpace(in.Value)
		if value == "" {
			return nil, &validationError{details: []string{fmt.Sprintf("types/statuses[%d]: value is required", index)}}
		}
		if len(value) > maxOptionValueLen {
			return nil, &validationError{details: []string{
				fmt.Sprintf("%s: value must not exceed %d characters", value, maxOptionValueLen)}}
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return nil, &validationError{details: []string{fmt.Sprintf("duplicate option value %q", value)}}
		}
		seen[key] = struct{}{}

		labels := localizedLabels{}
		if in.Labels != nil {
			labels = *in.Labels
		}
		labels.EN = strings.TrimSpace(labels.EN)
		labels.CS = strings.TrimSpace(labels.CS)
		labels.IT = strings.TrimSpace(labels.IT)
		if fallback := strings.TrimSpace(in.Label); labels.EN == "" {
			// A single `label` is the English label; the other languages stay
			// empty so the API falls back to English when one is requested.
			labels.EN = fallback
		}
		if labels.EN == "" {
			labels.EN = firstNonEmpty(labels.CS, labels.IT)
		}
		if labels.EN == "" {
			return nil, &validationError{details: []string{
				fmt.Sprintf("%s: a label (labels.en or label) is required", value)}}
		}

		if fixed := statusnames.Fixed(value); withColor && fixed != "" {
			labels = localizedLabels{EN: fixed, CS: fixed, IT: fixed}
		}
		color := strings.TrimSpace(in.Color)
		if withColor {
			if color == "" {
				color = defaultStatusColor
			}
			if !hexColorPattern.MatchString(color) {
				return nil, &validationError{details: []string{
					fmt.Sprintf("%s: color must be a hex value such as #93c47d", value)}}
			}
		} else {
			color = ""
		}

		out = append(out, normalizedOption{Value: value, Labels: labels, Color: color, SortOrder: index})
	}
	return out, nil
}

// applyOptionSet reconciles one option table with the desired set.
func (a *API) applyOptionSet(ctx context.Context, tx *sql.Tx, table string, existing []optionRow, desired []normalizedOption, renames map[string]string, withColor bool) error {
	// The byValue map holds pointers into existing, so existing must not be
	// reordered or appended to for as long as the map lives.
	byValue := make(map[string]*optionRow, len(existing))
	for index := range existing {
		row := &existing[index]
		byValue[strings.ToLower(row.Value)] = row
	}

	renamesByNew, err := reverseRenames(renames)
	if err != nil {
		return err
	}
	desiredValues := make(map[string]struct{}, len(desired))
	for _, option := range desired {
		desiredValues[strings.ToLower(option.Value)] = struct{}{}
	}
	used := make(map[int64]struct{}, len(desired))

	// Apply renames first: they move an existing row to a new value in place,
	// which is what keeps items.type_id / items.status_id valid.
	for _, newValue := range sortedKeys(renamesByNew) {
		oldValue := renamesByNew[newValue]
		if _, ok := desiredValues[strings.ToLower(newValue)]; !ok {
			return &validationError{details: []string{
				fmt.Sprintf("renames: %q is not part of the submitted %s", newValue, table)}}
		}
		row, ok := byValue[strings.ToLower(oldValue)]
		if !ok {
			return &validationError{details: []string{
				fmt.Sprintf("renames: %q is not an existing value in %s", oldValue, table)}}
		}
		if other, clash := byValue[strings.ToLower(newValue)]; clash && other.ID != row.ID {
			return &validationError{details: []string{
				fmt.Sprintf("renames: %q already exists in %s", newValue, table)}}
		}
		delete(byValue, strings.ToLower(oldValue))
		row.Value = newValue
		byValue[strings.ToLower(newValue)] = row
	}

	for _, option := range desired {
		if row, ok := byValue[strings.ToLower(option.Value)]; ok {
			used[row.ID] = struct{}{}
			if err := a.updateOptionRow(ctx, tx, table, row.ID, option, withColor); err != nil {
				return err
			}
			continue
		}
		if err := a.insertOptionRow(ctx, tx, table, option, withColor); err != nil {
			return err
		}
	}

	// Anything left over is being removed. Refuse to break items that still
	// reference it: the admin must rename the option or re-assign those items.
	for index := range existing {
		row := &existing[index]
		if _, ok := used[row.ID]; ok {
			continue
		}
		count, err := optionUsage(ctx, tx, table, row.ID)
		if err != nil {
			return err
		}
		if count > 0 {
			return &optionInUseError{table: table, value: row.Value, count: count}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE id = ?`, row.ID); err != nil {
			return fmt.Errorf("api: delete %s option: %w", table, err)
		}
	}
	return nil
}

// reverseRenames maps newValue (lower-cased) -> oldValue for a rename map.
// Two renames targeting the same value are rejected: the intent is ambiguous.
func reverseRenames(renames map[string]string) (map[string]string, error) {
	if len(renames) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(renames))
	for oldValue, newValue := range renames {
		oldValue = strings.TrimSpace(oldValue)
		newValue = strings.TrimSpace(newValue)
		if oldValue == "" || newValue == "" {
			return nil, &validationError{details: []string{"renames: both the old and the new value are required"}}
		}
		key := strings.ToLower(newValue)
		if existing, ok := out[key]; ok && !strings.EqualFold(existing, oldValue) {
			return nil, &validationError{details: []string{
				fmt.Sprintf("renames: %q and %q both rename to %q", existing, oldValue, newValue)}}
		}
		out[key] = oldValue
	}
	return out, nil
}

// sortedKeys returns the keys of m in a stable order.
func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// updateOptionRow writes one option row.
func (a *API) updateOptionRow(ctx context.Context, tx *sql.Tx, table string, id int64, option normalizedOption, withColor bool) error {
	now := a.now().UTC().Format(time.RFC3339)
	query := `UPDATE ` + table + ` SET value = ?, label_en = ?, label_cs = ?, label_it = ?, sort_order = ?, updated_at = ? WHERE id = ?`
	args := []any{option.Value, option.Labels.EN, option.Labels.CS, option.Labels.IT, option.SortOrder, now, id}
	if withColor {
		query = `UPDATE ` + table + ` SET value = ?, label_en = ?, label_cs = ?, label_it = ?, color = ?, sort_order = ?, updated_at = ? WHERE id = ?`
		args = []any{option.Value, option.Labels.EN, option.Labels.CS, option.Labels.IT, option.Color, option.SortOrder, now, id}
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("api: update %s option: %w", table, err)
	}
	return nil
}

// insertOptionRow adds a new option row.
func (a *API) insertOptionRow(ctx context.Context, tx *sql.Tx, table string, option normalizedOption, withColor bool) error {
	now := a.now().UTC().Format(time.RFC3339)
	query := `INSERT INTO ` + table + ` (value, label_en, label_cs, label_it, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	args := []any{option.Value, option.Labels.EN, option.Labels.CS, option.Labels.IT, option.SortOrder, now, now}
	if withColor {
		query = `INSERT INTO ` + table + ` (value, label_en, label_cs, label_it, color, sort_order, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
		args = []any{option.Value, option.Labels.EN, option.Labels.CS, option.Labels.IT, option.Color, option.SortOrder, now, now}
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("api: insert %s option: %w", table, err)
	}
	return nil
}

// optionUsage counts the items referencing an option row. Archived items count
// too: removing an option they reference would erase part of their history.
func optionUsage(ctx context.Context, q queryer, table string, id int64) (int, error) {
	column := "type_id"
	if table == "statuses" {
		column = "status_id"
	}
	var count int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE `+column+` = ?`, id).Scan(&count); err != nil {
		return 0, fmt.Errorf("api: count option usage: %w", err)
	}
	return count, nil
}

// optionInUseError reports an attempt to delete an option that items reference.
type optionInUseError struct {
	table string
	value string
	count int
}

func (e *optionInUseError) Error() string {
	kind := "type"
	if e.table == "statuses" {
		kind = "status"
	}
	return fmt.Sprintf("%s %q is still used by %d item(s)", kind, e.value, e.count)
}

// writeOptionsError maps option errors onto the error envelope.
func (a *API) writeOptionsError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *validationError
	var inUse *optionInUseError

	switch {
	case errors.As(err, &inUse):
		Error(w, r, http.StatusConflict, codeConflict, "option is still in use", inUse.Error())
	case errors.As(err, &validation):
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, validation.Error(), validation.details...)
	default:
		InternalError(w, r, a.log, err)
	}
}

// firstNonEmpty returns the first non-blank value.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
