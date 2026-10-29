package api

// End-to-end integration: the legacy JSON is imported with the real importer
// CLI, and the API is then exercised against that data. This is the chain that
// matters for the migration (legacy file → SQLite → HTTP).

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// legacyFixture is the subset of the legacy JSON the assertions need.
type legacyFixture struct {
	Items []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		SN       string `json:"sn"`
		Type     string `json:"type"`
		Status   string `json:"status"`
		Note     string `json:"note"`
		SentDate string `json:"sentDate"`
		Return   string `json:"returnDate"`
	} `json:"items"`
	Types    []string `json:"types"`
	Statuses []struct {
		Value string `json:"value"`
		Label string `json:"label"`
		Cls   string `json:"cls"`
	} `json:"statuses"`
}

// repoRoot resolves the repository root from this package directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root, err := filepath.Abs(filepath.Join(wd, "..", "..", ".."))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skipf("repository root not found from %s (%v)", wd, err)
	}
	return root
}

// legacyDataDir locates equipment-log/app/data (the source of the migration).
func legacyDataDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(repoRoot(t), "equipment-log", "app", "data")
	if _, err := os.Stat(filepath.Join(dir, "inventory.json")); err != nil {
		t.Skipf("legacy data not present: %v", err)
	}
	return dir
}

// loadLegacyFixture reads and parses the legacy inventory and options files.
func loadLegacyFixture(t *testing.T, dir string) legacyFixture {
	t.Helper()
	var fixture legacyFixture
	inventory, err := os.ReadFile(filepath.Join(dir, "inventory.json"))
	if err != nil {
		t.Fatalf("read legacy inventory: %v", err)
	}
	if err := json.Unmarshal(inventory, &fixture.Items); err != nil {
		t.Fatalf("decode legacy inventory: %v", err)
	}
	options, err := os.ReadFile(filepath.Join(dir, "options.json"))
	if err != nil {
		t.Fatalf("read legacy options: %v", err)
	}
	if err := json.Unmarshal(options, &fixture); err != nil {
		t.Fatalf("decode legacy options: %v", err)
	}
	if len(fixture.Items) == 0 {
		t.Fatal("the legacy inventory contains no items")
	}
	return fixture
}

// runImporter executes the real importer CLI against the legacy data.
func runImporter(t *testing.T, dbPath, dataDir, sourceDir string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping the importer integration test in short mode")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("the go toolchain is not on PATH: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, goBin, "run", "./server/cmd/import-json",
		"-db", dbPath,
		"-data-dir", dataDir,
		"-source", sourceDir,
		"-pre-snapshot=false",
	)
	cmd.Dir = repoRoot(t)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		t.Fatalf("importer failed: %v\n%s", err, output.String())
	}
	return output.String()
}

func TestLegacyImportEndToEnd(t *testing.T) {
	t.Parallel()

	dir := legacyDataDir(t)
	fixture := loadLegacyFixture(t, dir)

	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "sklad.db")

	output := runImporter(t, dbPath, dataDir, dir)
	if !strings.Contains(output, "import") {
		t.Logf("importer output: %s", output)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("the importer did not create the database: %v", err)
	}

	env := newTestEnv(t, withDB(dbPath))

	// Re-running the importer must be a no-op: the migration is idempotent.
	before := env.countQuery(`SELECT COUNT(*) FROM items`)
	runImporter(t, dbPath, dataDir, dir)
	env = newTestEnv(t, withDB(dbPath))
	if after := env.countQuery(`SELECT COUNT(*) FROM items`); after != before {
		t.Fatalf("a second import changed the item count: %d -> %d", before, after)
	}

	// Every legacy tag survives verbatim, including the intentional gap.
	list := env.listItems("?per_page=1000")
	if list.Meta.Total != len(fixture.Items) {
		t.Fatalf("expected %d imported items, got %d", len(fixture.Items), list.Meta.Total)
	}
	byTag := make(map[string]Item, len(list.Data))
	for _, item := range list.Data {
		byTag[item.AssetTag] = item
	}
	for _, legacy := range fixture.Items {
		item, ok := byTag[legacy.ID]
		if !ok {
			t.Fatalf("legacy tag %s is missing from the API", legacy.ID)
		}
		if item.Name != legacy.Name || item.SN != legacy.SN || item.Note != legacy.Note {
			t.Fatalf("legacy fields changed for %s: %+v", legacy.ID, item)
		}
		// Legacy empty strings become NULL dates, and dates stay as imported.
		wantSent := legacy.SentDate
		if wantSent == "" {
			if item.SentDate != nil {
				t.Fatalf("%s: expected no sent_date, got %v", legacy.ID, *item.SentDate)
			}
		} else if item.SentDate == nil || *item.SentDate != wantSent {
			t.Fatalf("%s: sent_date mismatch (%v)", legacy.ID, item.SentDate)
		}
		wantReturn := legacy.Return
		if wantReturn == "" {
			if item.ReturnDate != nil {
				t.Fatalf("%s: expected no return_date, got %v", legacy.ID, *item.ReturnDate)
			}
		} else if item.ReturnDate == nil || *item.ReturnDate != wantReturn {
			t.Fatalf("%s: return_date mismatch (%v)", legacy.ID, item.ReturnDate)
		}
	}
	if _, present := byTag["PV-049"]; present {
		t.Fatal("the intentional PV-049 gap was filled by the import")
	}

	// The option sets arrive with their colours and localized labels.
	payload := env.admin.mustDo(http.MethodGet, "/api/v1/options?lang=cs", nil, http.StatusOK)
	options := decode[struct {
		Data struct {
			Types    []typeOption   `json:"types"`
			Statuses []statusOption `json:"statuses"`
		} `json:"data"`
	}](t, payload)
	if len(options.Data.Types) < len(fixture.Types) {
		t.Fatalf("expected at least %d types, got %d", len(fixture.Types), len(options.Data.Types))
	}
	if len(options.Data.Statuses) != len(fixture.Statuses) {
		t.Fatalf("expected %d statuses, got %d", len(fixture.Statuses), len(options.Data.Statuses))
	}
	for _, status := range options.Data.Statuses {
		if status.Color == "" || !strings.HasPrefix(status.Color, "#") {
			t.Fatalf("status %q lost its colour: %+v", status.Value, status)
		}
	}

	// The next free tag continues after the highest legacy number and never
	// reuses the gap.
	created := env.createItem(map[string]any{"name": "After the import"})
	if created.AssetTag != "PV-083" {
		t.Fatalf("the next tag should be PV-083 (max+1, gap preserved), got %q", created.AssetTag)
	}

	// Filtering and search work on the imported data.
	cameras := 0
	for _, legacy := range fixture.Items {
		if legacy.Type == "Camera" {
			cameras++
		}
	}
	if got := env.listItems("?type=Camera&per_page=1000").Meta.Total; got != cameras {
		t.Fatalf("type filter over imported data: expected %d cameras, got %d", cameras, got)
	}
	first := fixture.Items[0]
	if got := env.listItems("?q=" + urlQueryEscape(first.SN)).Meta.Total; got == 0 {
		t.Fatalf("searching for the legacy serial %q returned nothing", first.SN)
	}

	// Scanning a legacy label resolves to the imported item.
	payload = env.admin.mustDo(http.MethodPost, "/api/v1/items/resolve", map[string]any{
		"codes": []string{strings.ToLower(first.ID)},
	}, http.StatusOK)
	resolved := decode[struct {
		Data struct {
			Matched []Item   `json:"matched"`
			Unknown []string `json:"unknown"`
		} `json:"data"`
	}](t, payload)
	if len(resolved.Data.Matched) != 1 || resolved.Data.Matched[0].AssetTag != first.ID {
		t.Fatalf("scanning %s did not resolve: %+v", first.ID, resolved.Data)
	}

	// The CSV export contains every imported row.
	_, _, body := env.admin.doFull(http.MethodGet, "/api/v1/export?format=csv", nil)
	records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
	if err != nil {
		t.Fatalf("parse exported CSV: %v", err)
	}
	if len(records) != len(fixture.Items)+2 { // header + imported items + the new one
		t.Fatalf("CSV should hold every row: got %d records for %d items", len(records), len(fixture.Items))
	}
	tagColumn := -1
	for i, name := range records[0] {
		if name == "asset_tag" {
			tagColumn = i
		}
	}
	if tagColumn < 0 {
		t.Fatalf("CSV header has no asset_tag column: %v", records[0])
	}
	if records[1][tagColumn] != fixture.Items[0].ID {
		t.Fatalf("CSV row order/tag mismatch: %v", records[1])
	}
}

// urlQueryEscape keeps query building readable in the assertions above.
func urlQueryEscape(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), " ", "%20")
}

// TestLegacyImportDryRunCreatesNothing covers the safe pre-flight the runbook
// recommends before touching production data.
func TestLegacyImportDryRunCreatesNothing(t *testing.T) {
	t.Parallel()

	dir := legacyDataDir(t)
	fixture := loadLegacyFixture(t, dir)

	if testing.Short() {
		t.Skip("skipping the importer integration test in short mode")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("the go toolchain is not on PATH: %v", err)
	}

	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "dry-run.db")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBin, "run", "./server/cmd/import-json",
		"-db", dbPath, "-data-dir", dataDir, "-source", dir, "-dry-run")
	cmd.Dir = repoRoot(t)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		t.Fatalf("dry run failed: %v\n%s", err, output.String())
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("a dry run must not create the database (stat: %v)", err)
	}
	if !strings.Contains(output.String(), fmt.Sprintf("%d", len(fixture.Items))) {
		t.Logf("dry-run output did not mention the item count (%d): %s", len(fixture.Items), output.String())
	}
}
