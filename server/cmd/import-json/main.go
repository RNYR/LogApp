// Command import-json migrates the legacy Equipment Log JSON data
// (equipment-log/app/data/inventory.json and options.json) into the Sklad
// SQLite database.
//
// It is idempotent: every row is inserted with ON CONFLICT DO NOTHING, so it
// can be re-run safely (already-present rows are skipped, never overwritten).
// --dry-run validates and reports without touching the database, and
// --pre-snapshot takes a VACUUM INTO backup of the target database first.
//
// The asset tags are preserved verbatim (including the legacy PV-049 gap); no
// resequencing ever happens. Legacy "sentDate"/"returnDate" empty strings are
// mapped to SQL NULL.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sklad/server/internal/statusnames"
	"sort"
	"strconv"
	"strings"
	"time"

	"sklad/server/internal/store"
)

// ---------------------------------------------------------------------------
// Legacy JSON shapes (exact field names of the old app)
// ---------------------------------------------------------------------------

type legacyItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	SN         string `json:"sn"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	SentDate   string `json:"sentDate"`
	ReturnDate string `json:"returnDate"`
	Note       string `json:"note"`
}

type legacyStatus struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Cls   string `json:"cls"`
}

type legacyOptions struct {
	Types    []string       `json:"types"`
	Statuses []legacyStatus `json:"statuses"`
}

type legacyData struct {
	Items   []legacyItem
	Options legacyOptions
}

// ImportResult summarises what an import did.
type ImportResult struct {
	TypesImported    int
	TypesSkipped     int
	StatusesImported int
	StatusesSkipped  int
	ItemsImported    int
	ItemsSkipped     int
}

// ---------------------------------------------------------------------------
// Legacy status CSS class -> hex colour (mirrors the old frontend palette and
// the frozen schema default).
// ---------------------------------------------------------------------------

var clsToColor = map[string]string{
	"status-feas":     "#93c47d",
	"status-devel":    "#8faadc",
	"status-sales":    "#CC99FF",
	"status-customer": "#ffd966",
	"status-loaned":   "#e06666",
}

var customClsRe = regexp.MustCompile(`^status-custom-([0-9a-fA-F]{6})$`)

func statusColor(cls string) string {
	if color, ok := clsToColor[cls]; ok {
		return color
	}
	if m := customClsRe.FindStringSubmatch(cls); m != nil {
		return "#" + strings.ToLower(m[1])
	}
	return "#93c47d"
}

// ---------------------------------------------------------------------------
// CLI
// ---------------------------------------------------------------------------

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "import-json: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	dbPath := flag.String("db", defaultDBPath(), "target SQLite database path (SKLAD_DB_PATH)")
	dataDir := flag.String("data-dir", "", "data directory for backups (default: directory of -db)")
	source := flag.String("source", defaultSourceDir(), "directory containing inventory.json and options.json")
	dryRun := flag.Bool("dry-run", false, "validate and report without opening or writing the database")
	preSnapshot := flag.Bool("pre-snapshot", true, "take a VACUUM INTO backup of the target database before importing")
	flag.Parse()

	legacy, err := readLegacy(*source)
	if err != nil {
		return err
	}
	if err := validateLegacy(legacy); err != nil {
		return err
	}

	if *dryRun {
		printDryRun(legacy)
		return nil
	}

	if *dataDir == "" {
		*dataDir = filepath.Dir(*dbPath)
	}

	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DBPath: *dbPath, DataDir: *dataDir})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	if *preSnapshot {
		backup, err := st.BackupDB(ctx)
		if err != nil {
			return fmt.Errorf("pre-snapshot: %w", err)
		}
		fmt.Printf("pre-snapshot: %s\n", backup)
	}

	res, err := runImport(ctx, st, legacy)
	if err != nil {
		return err
	}

	fmt.Printf("Import summary:\n")
	fmt.Printf("  item types:   %d imported, %d already present\n", res.TypesImported, res.TypesSkipped)
	fmt.Printf("  statuses:     %d imported, %d already present\n", res.StatusesImported, res.StatusesSkipped)
	fmt.Printf("  items:        %d imported, %d already present\n", res.ItemsImported, res.ItemsSkipped)
	return nil
}

func defaultDBPath() string {
	if v := os.Getenv("SKLAD_DB_PATH"); v != "" {
		return v
	}
	return "sklad.db"
}

func defaultSourceDir() string {
	return filepath.Join("equipment-log", "app", "data")
}

// ---------------------------------------------------------------------------
// Reading & validation
// ---------------------------------------------------------------------------

func readLegacy(dir string) (*legacyData, error) {
	invPath := filepath.Join(dir, "inventory.json")
	invBytes, err := os.ReadFile(invPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", invPath, err)
	}
	var items []legacyItem
	if err := json.Unmarshal(invBytes, &items); err != nil {
		return nil, fmt.Errorf("parse %s: %w", invPath, err)
	}

	// options.json is optional: when absent, types/statuses are derived from
	// the items themselves.
	legacy := &legacyData{Items: items}
	optPath := filepath.Join(dir, "options.json")
	if optBytes, err := os.ReadFile(optPath); err == nil {
		if err := json.Unmarshal(optBytes, &legacy.Options); err != nil {
			return nil, fmt.Errorf("parse %s: %w", optPath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", optPath, err)
	}
	return legacy, nil
}

func validateLegacy(legacy *legacyData) error {
	seen := make(map[string]string, len(legacy.Items))
	for _, item := range legacy.Items {
		tag := strings.TrimSpace(item.ID)
		if tag == "" {
			return errors.New("inventory.json contains an item with an empty id")
		}
		key := strings.ToLower(tag)
		if previous, ok := seen[key]; ok {
			return fmt.Errorf("inventory.json contains duplicate asset tag %q (first seen as %q)", item.ID, previous)
		}
		seen[key] = item.ID
	}
	return nil
}

func printDryRun(legacy *legacyData) {
	types := collectTypes(legacy)
	statuses := collectStatuses(legacy)
	fmt.Printf("Dry run (no database access):\n")
	fmt.Printf("  source items:      %d\n", len(legacy.Items))
	fmt.Printf("  distinct types:    %d\n", len(types))
	fmt.Printf("  distinct statuses: %d\n", len(statuses))
	if missing := missingTagNumbers(legacy.Items); len(missing) > 0 {
		fmt.Printf("  tag gaps (missing): %v\n", missing)
	}
}

// missingTagNumbers returns the PV numbers absent between the lowest and
// highest present tags (for example [49] for the legacy data).
func missingTagNumbers(items []legacyItem) []int {
	var numbers []int
	for _, item := range items {
		if n, ok := parseTagNumber(item.ID); ok {
			numbers = append(numbers, n)
		}
	}
	if len(numbers) < 2 {
		return nil
	}
	sort.Ints(numbers)
	min, max := numbers[0], numbers[len(numbers)-1]
	present := make(map[int]struct{}, len(numbers))
	for _, n := range numbers {
		present[n] = struct{}{}
	}
	var missing []int
	for n := min; n <= max; n++ {
		if _, ok := present[n]; !ok {
			missing = append(missing, n)
		}
	}
	return missing
}

func parseTagNumber(tag string) (int, bool) {
	tag = strings.TrimSpace(tag)
	if !strings.HasPrefix(strings.ToUpper(tag), "PV-") {
		return 0, false
	}
	n, err := strconv.Atoi(tag[3:])
	if err != nil {
		return 0, false
	}
	return n, true
}

// ---------------------------------------------------------------------------
// Import
// ---------------------------------------------------------------------------

// runImport inserts everything inside a single transaction so a failure leaves
// the database unchanged. It never overwrites existing rows (idempotent).
func runImport(ctx context.Context, st *store.Store, legacy *legacyData) (ImportResult, error) {
	var res ImportResult
	err := st.WithTx(ctx, func(tx *sql.Tx) error {
		typeIDs, err := importTypes(ctx, tx, collectTypes(legacy), &res)
		if err != nil {
			return err
		}
		statusIDs, err := importStatuses(ctx, tx, collectStatuses(legacy), &res)
		if err != nil {
			return err
		}
		return importItems(ctx, tx, legacy.Items, typeIDs, statusIDs, &res)
	})
	if err != nil {
		return res, err
	}
	return res, nil
}

// collectTypes is the ordered, de-duplicated union of the option types and the
// types actually used by items, so nothing is lost even if options.json is
// incomplete.
func collectTypes(legacy *legacyData) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	for _, t := range legacy.Options.Types {
		add(t)
	}
	for _, item := range legacy.Items {
		add(item.Type)
	}
	return out
}

// collectStatuses is the ordered, de-duplicated union of option statuses and
// the statuses actually used by items. Item-only statuses get the default
// colour and their value as the label.
func collectStatuses(legacy *legacyData) []legacyStatus {
	seen := make(map[string]struct{})
	var out []legacyStatus
	add := func(s legacyStatus) {
		s.Value = strings.TrimSpace(s.Value)
		if s.Value == "" {
			return
		}
		if _, ok := seen[s.Value]; ok {
			return
		}
		seen[s.Value] = struct{}{}
		out = append(out, s)
	}
	for _, s := range legacy.Options.Statuses {
		add(s)
	}
	for _, item := range legacy.Items {
		value := strings.TrimSpace(item.Status)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		add(legacyStatus{Value: value, Label: value, Cls: ""})
	}
	return out
}

func importTypes(ctx context.Context, tx *sql.Tx, types []string, res *ImportResult) (map[string]int64, error) {
	for i, value := range types {
		now := time.Now().UTC().Format(time.RFC3339)
		r, err := tx.ExecContext(ctx,
			`INSERT INTO item_types (value, label_en, label_cs, label_it, sort_order, created_at, updated_at)
			 VALUES (?, ?, '', '', ?, ?, ?)
			 ON CONFLICT(value) DO NOTHING`,
			value, value, i, now, now,
		)
		if err != nil {
			return nil, fmt.Errorf("insert item type %q: %w", value, err)
		}
		if n, _ := r.RowsAffected(); n > 0 {
			res.TypesImported++
		} else {
			res.TypesSkipped++
		}
	}
	return loadValueIDs(ctx, tx, "item_types")
}

func importStatuses(ctx context.Context, tx *sql.Tx, statuses []legacyStatus, res *ImportResult) (map[string]int64, error) {
	for i, s := range statuses {
		label := s.Label
		if fixed := statusnames.Fixed(s.Value); fixed != "" {
			label = fixed
		}
		if strings.TrimSpace(label) == "" {
			label = s.Value
		}
		now := time.Now().UTC().Format(time.RFC3339)
		r, err := tx.ExecContext(ctx,
			`INSERT INTO statuses (value, label_en, label_cs, label_it, color, sort_order, created_at, updated_at)
			 VALUES (?, ?, '', '', ?, ?, ?, ?)
			 ON CONFLICT(value) DO NOTHING`,
			s.Value, label, statusColor(s.Cls), i, now, now,
		)
		if err != nil {
			return nil, fmt.Errorf("insert status %q: %w", s.Value, err)
		}
		if n, _ := r.RowsAffected(); n > 0 {
			res.StatusesImported++
		} else {
			res.StatusesSkipped++
		}
	}
	return loadValueIDs(ctx, tx, "statuses")
}

// loadValueIDs returns a map from the unique value column to the row id for a
// value-keyed options table, including rows imported earlier.
func loadValueIDs(ctx context.Context, tx *sql.Tx, table string) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, value FROM `+table)
	if err != nil {
		return nil, fmt.Errorf("load %s ids: %w", table, err)
	}
	defer rows.Close()
	ids := make(map[string]int64)
	for rows.Next() {
		var id int64
		var value string
		if err := rows.Scan(&id, &value); err != nil {
			return nil, fmt.Errorf("scan %s: %w", table, err)
		}
		ids[value] = id
	}
	return ids, rows.Err()
}

func importItems(ctx context.Context, tx *sql.Tx, items []legacyItem, typeIDs, statusIDs map[string]int64, res *ImportResult) error {
	for _, item := range items {
		tag := strings.TrimSpace(item.ID)
		var typeID, statusID any
		if id, ok := typeIDs[strings.TrimSpace(item.Type)]; ok && strings.TrimSpace(item.Type) != "" {
			typeID = id
		}
		if id, ok := statusIDs[strings.TrimSpace(item.Status)]; ok && strings.TrimSpace(item.Status) != "" {
			statusID = id
		}
		now := time.Now().UTC().Format(time.RFC3339)
		r, err := tx.ExecContext(ctx,
			`INSERT INTO items (asset_tag, name, sn, type_id, status_id, sent_date, return_date, note, version, archived, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 0, ?, ?)
			 ON CONFLICT(asset_tag) DO NOTHING`,
			tag, item.Name, item.SN, typeID, statusID,
			nullIfEmpty(item.SentDate), nullIfEmpty(item.ReturnDate), item.Note,
			now, now,
		)
		if err != nil {
			return fmt.Errorf("insert item %q: %w", tag, err)
		}
		if n, _ := r.RowsAffected(); n > 0 {
			res.ItemsImported++
		} else {
			res.ItemsSkipped++
		}
	}
	return nil
}

// nullIfEmpty converts the legacy "" sentinel into SQL NULL.
func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
