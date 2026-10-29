package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sklad/server/internal/store"
)

// dbItem is an item as read back from SQLite, with type/status resolved to
// their values and NULL dates collapsed to "".
type dbItem struct {
	AssetTag   string
	Name       string
	SN         string
	Type       string
	Status     string
	SentDate   string
	ReturnDate string
	Note       string
}

func dumpItems(t *testing.T, st *store.Store) map[string]dbItem {
	t.Helper()
	rows, err := st.DB().QueryContext(context.Background(), `
		SELECT i.asset_tag, i.name, i.sn,
		       COALESCE(t.value, ''), COALESCE(s.value, ''),
		       i.sent_date, i.return_date, i.note
		FROM items i
		LEFT JOIN item_types t ON t.id = i.type_id
		LEFT JOIN statuses s   ON s.id = i.status_id
		ORDER BY i.asset_tag`)
	if err != nil {
		t.Fatalf("dump items: %v", err)
	}
	defer rows.Close()

	out := make(map[string]dbItem)
	for rows.Next() {
		var it dbItem
		var sent, ret sql.NullString
		if err := rows.Scan(&it.AssetTag, &it.Name, &it.SN, &it.Type, &it.Status, &sent, &ret, &it.Note); err != nil {
			t.Fatalf("scan item: %v", err)
		}
		if sent.Valid {
			it.SentDate = sent.String
		}
		if ret.Valid {
			it.ReturnDate = ret.String
		}
		out[it.AssetTag] = it
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// assertLossless diffs the imported rows against the legacy source items.
func assertLossless(t *testing.T, legacy []legacyItem, got map[string]dbItem) {
	t.Helper()
	if len(got) != len(legacy) {
		t.Fatalf("item count mismatch: legacy=%d db=%d", len(legacy), len(got))
	}
	for _, src := range legacy {
		tag := strings.TrimSpace(src.ID)
		db, ok := got[tag]
		if !ok {
			t.Errorf("asset tag %q missing from database", tag)
			continue
		}
		if db.Name != src.Name {
			t.Errorf("%s name: got %q want %q", tag, db.Name, src.Name)
		}
		if db.SN != src.SN {
			t.Errorf("%s sn: got %q want %q", tag, db.SN, src.SN)
		}
		if db.Type != src.Type {
			t.Errorf("%s type: got %q want %q", tag, db.Type, src.Type)
		}
		if db.Status != src.Status {
			t.Errorf("%s status: got %q want %q", tag, db.Status, src.Status)
		}
		if db.Note != src.Note {
			t.Errorf("%s note: got %q want %q", tag, db.Note, src.Note)
		}
		if db.SentDate != src.SentDate {
			t.Errorf("%s sentDate: got %q want %q", tag, db.SentDate, src.SentDate)
		}
		if db.ReturnDate != src.ReturnDate {
			t.Errorf("%s returnDate: got %q want %q", tag, db.ReturnDate, src.ReturnDate)
		}
	}
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(context.Background(), store.Options{DBPath: filepath.Join(dir, "sklad.db"), DataDir: dir})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func writeLegacyJSON(t *testing.T, dir string, items []legacyItem, opts legacyOptions) {
	t.Helper()
	inv, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "inventory.json"), inv, 0o644); err != nil {
		t.Fatal(err)
	}
	opt, err := json.Marshal(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "options.json"), opt, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestImportSyntheticLossless exercises the importer end-to-end against a
// hermetic fixture: a tag gap, item-only options (auto-created), Czech/em-dash
// notes, empty dates and a custom status colour.
func TestImportSyntheticLossless(t *testing.T) {
	source := t.TempDir()
	items := []legacyItem{
		{ID: "PV-001", Name: "Camera A", SN: "SN-1", Type: "Camera", Status: "Feasibility", SentDate: "", ReturnDate: "", Note: ""},
		{ID: "PV-002", Name: "Lens B", SN: "", Type: "Lens", Status: "Loaned", SentDate: "2026-01-02", ReturnDate: "2026-02-03", Note: "Gřeš"},
		{ID: "PV-004", Name: "Cable C", SN: "X", Type: "Cable", Status: "Customer", SentDate: "", ReturnDate: "", Note: "—"},
		{ID: "PV-005", Name: "Cam D", SN: "SN-5", Type: "Camera", Status: "Rented", SentDate: "", ReturnDate: "", Note: ""},
	}
	opts := legacyOptions{
		Types: []string{"Camera", "Lens"},
		Statuses: []legacyStatus{
			{Value: "Feasibility", Label: "Feasibility", Cls: "status-feas"},
			{Value: "Loaned", Label: "Loaned", Cls: "status-loaned"},
			{Value: "Rented", Label: "Rented", Cls: "status-custom-ff0000"},
		},
	}
	writeLegacyJSON(t, source, items, opts)

	legacy, err := readLegacy(source)
	if err != nil {
		t.Fatalf("readLegacy: %v", err)
	}
	if err := validateLegacy(legacy); err != nil {
		t.Fatalf("validateLegacy: %v", err)
	}

	st := openTestStore(t)
	res, err := runImport(context.Background(), st, legacy)
	if err != nil {
		t.Fatalf("runImport: %v", err)
	}
	if res.ItemsImported != 4 {
		t.Fatalf("items imported = %d, want 4", res.ItemsImported)
	}

	assertLossless(t, items, dumpItems(t, st))

	// The PV-003 gap is preserved (no resequencing).
	got := dumpItems(t, st)
	if _, ok := got["PV-003"]; ok {
		t.Fatal("PV-003 must remain absent")
	}
	if _, ok := got["PV-004"]; !ok {
		t.Fatal("PV-004 missing")
	}

	// Item-only options were auto-created, custom colour extracted.
	var cableColor string
	if err := st.DB().QueryRowContext(context.Background(),
		`SELECT color FROM statuses WHERE value = 'Rented'`).Scan(&cableColor); err != nil {
		t.Fatalf("Rented status color: %v", err)
	}
	if cableColor != "#ff0000" {
		t.Fatalf("Rented color = %q, want #ff0000", cableColor)
	}
	var customerColor string
	if err := st.DB().QueryRowContext(context.Background(),
		`SELECT color FROM statuses WHERE value = 'Customer'`).Scan(&customerColor); err != nil {
		t.Fatalf("Customer status missing (auto-create): %v", err)
	}
	if customerColor != "#93c47d" {
		t.Fatalf("Customer default color = %q, want #93c47d", customerColor)
	}

	// Idempotency: a second run imports nothing.
	res2, err := runImport(context.Background(), st, legacy)
	if err != nil {
		t.Fatalf("runImport (2nd): %v", err)
	}
	if res2.ItemsImported != 0 || res2.TypesImported != 0 || res2.StatusesImported != 0 {
		t.Fatalf("second import not idempotent: %+v", res2)
	}
	if len(dumpItems(t, st)) != 4 {
		t.Fatal("item count changed after re-import")
	}
}

// TestImportRealLegacyData performs the lossless diff against the real
// equipment-log/app/data files when they are present (they are part of this
// repository). It asserts the exact item count, the PV-049 gap and byte-identical
// round-trip of every field.
func TestImportRealLegacyData(t *testing.T) {
	// package dir is server/cmd/import-json; the legacy data lives three levels up.
	source := filepath.Join("..", "..", "..", "equipment-log", "app", "data")
	if _, err := os.Stat(filepath.Join(source, "inventory.json")); err != nil {
		t.Skipf("legacy data not available at %s: %v", source, err)
	}

	legacy, err := readLegacy(source)
	if err != nil {
		t.Fatalf("readLegacy: %v", err)
	}
	if err := validateLegacy(legacy); err != nil {
		t.Fatalf("validateLegacy: %v", err)
	}
	if len(legacy.Items) == 0 {
		t.Fatal("legacy data is empty")
	}

	st := openTestStore(t)
	res, err := runImport(context.Background(), st, legacy)
	if err != nil {
		t.Fatalf("runImport: %v", err)
	}
	if res.ItemsImported != len(legacy.Items) {
		t.Fatalf("items imported = %d, want %d", res.ItemsImported, len(legacy.Items))
	}

	got := dumpItems(t, st)
	assertLossless(t, legacy.Items, got)

	// The tag set must be identical: no resequencing, no dropped tags.
	legacyTags := make([]string, 0, len(legacy.Items))
	for _, it := range legacy.Items {
		legacyTags = append(legacyTags, strings.TrimSpace(it.ID))
	}
	dbTags := make([]string, 0, len(got))
	for tag := range got {
		dbTags = append(dbTags, tag)
	}
	sort.Strings(legacyTags)
	sort.Strings(dbTags)
	if strings.Join(legacyTags, ",") != strings.Join(dbTags, ",") {
		t.Fatalf("tag sets differ:\n legacy=%v\n db    =%v", legacyTags, dbTags)
	}
	if _, ok := got["PV-049"]; ok {
		t.Fatal("PV-049 must remain absent (legacy gap preserved)")
	}

	// Idempotency.
	res2, err := runImport(context.Background(), st, legacy)
	if err != nil {
		t.Fatalf("runImport (2nd): %v", err)
	}
	if res2.ItemsImported != 0 {
		t.Fatalf("second import imported %d items, want 0", res2.ItemsImported)
	}
	if len(dumpItems(t, st)) != len(legacy.Items) {
		t.Fatal("item count changed after re-import")
	}
}

// TestValidateLegacyDuplicate rejects duplicate tags so a lossless import
// cannot silently drop a row.
func TestValidateLegacyDuplicate(t *testing.T) {
	legacy := &legacyData{Items: []legacyItem{{ID: "PV-001"}, {ID: "pv-001"}}}
	if err := validateLegacy(legacy); err == nil {
		t.Fatal("expected duplicate tag error")
	}
}
