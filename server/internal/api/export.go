package api

// Inventory export: the same item data as GET /items, without pagination, as a
// JSON document or as a spreadsheet-friendly CSV file.

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Export formats accepted by ?format=.
const (
	exportFormatJSON = "json"
	exportFormatCSV  = "csv"
)

// csvColumns is the fixed column order of the CSV export. It is a complete flat
// dump of an item, including the type/status machine keys (not the localized
// labels, which would make the file locale-dependent).
var csvColumns = []string{
	"id", "asset_tag", "name", "sn", "type", "status",
	"sent_date", "return_date", "note", "archived", "version",
	"label_printed_at", "created_at", "updated_at", "created_by", "updated_by", "personnel_id", "personnel_name",
}

// exportDocument is the JSON export body.
type exportDocument struct {
	ExportedAt string `json:"exported_at"`
	Total      int    `json:"total"`
	Items      []Item `json:"items"`
}

// mountExportRoutes registers the export endpoint on the public router.
// Exporting reads the same equipment available in public inventory.
func (a *API) mountExportRoutes(r chi.Router) {
	r.Get("/export", a.handleExport)
}

// handleExport implements GET /api/v1/export.
func (a *API) handleExport(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	format := strings.ToLower(strings.TrimSpace(query.Get("format")))
	if format == "" {
		format = exportFormatJSON
	}
	if format != exportFormatJSON && format != exportFormatCSV {
		Error(w, r, http.StatusBadRequest, CodeValidationFailed,
			fmt.Sprintf("format must be %s or %s", exportFormatJSON, exportFormatCSV))
		return
	}

	archived, err := boolParam(query.Get("archived"), false)
	if err != nil {
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, fmt.Sprintf("archived: %s", err))
		return
	}

	// One unpaginated page: the export must contain everything that matches.
	filter := itemFilter{
		Archived: archived,
		Sort:     "asset_tag",
		Order:    sortAsc,
		Limit:    maxPerPage,
		Locale:   requestLocale(r),
		Today:    a.today(),
	}

	items, total, err := a.queryItems(r.Context(), filter)
	if err != nil {
		InternalError(w, r, a.log, err)
		return
	}
	// Page through the remaining rows so a large inventory is never silently
	// truncated. The first page is already in hand, so continue after it.
	for offset := len(items); offset < total; offset += filter.Limit {
		filter.Offset = offset
		page, _, err := a.queryItems(r.Context(), filter)
		if err != nil {
			InternalError(w, r, a.log, err)
			return
		}
		if len(page) == 0 {
			break
		}
		items = append(items, page...)
	}

	filename := fmt.Sprintf("sklad-items-%s.%s", a.now().UTC().Format("20060102"), format)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")

	if format == exportFormatCSV {
		a.writeCSV(w, items)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"data": exportDocument{
		ExportedAt: a.now().UTC().Format(time.RFC3339),
		Total:      len(items),
		Items:      items,
	}})
}

// writeCSV renders items as CSV. A UTF-8 BOM is emitted so spreadsheet
// applications detect the encoding, and rows use CRLF as RFC 4180 requires.
func (a *API) writeCSV(w http.ResponseWriter, items []Item) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return
	}

	writer := csv.NewWriter(w)
	writer.UseCRLF = true
	if err := writer.Write(csvColumns); err != nil {
		a.log.Warn("failed to write CSV header", "error", err)
		return
	}
	for _, item := range items {
		if err := writer.Write(csvRow(item)); err != nil {
			a.log.Warn("failed to write CSV row", "error", err)
			return
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		a.log.Warn("failed to flush CSV export", "error", err)
	}
}

// csvRow renders one item in csvColumns order.
func csvRow(item Item) []string {
	return []string{
		strconv.FormatInt(item.ID, 10),
		item.AssetTag,
		item.Name,
		item.SN,
		valueOrEmpty(item.Type),
		valueOrEmpty(item.Status),
		valueOrEmpty(item.SentDate),
		valueOrEmpty(item.ReturnDate),
		item.Note,
		strconv.FormatBool(item.Archived),
		strconv.FormatInt(item.Version, 10),
		valueOrEmpty(item.LabelPrintedAt),
		item.CreatedAt,
		item.UpdatedAt,
		valueOrEmpty(item.CreatedBy),
		valueOrEmpty(item.UpdatedBy),
		personnelIDString(item.PersonnelID), valueOrEmpty(item.PersonnelName),
	}
}

// valueOrEmpty dereferences an optional string for CSV output.
func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func personnelIDString(id *int64) string {
	if id == nil {
		return ""
	}
	return strconv.FormatInt(*id, 10)
}
