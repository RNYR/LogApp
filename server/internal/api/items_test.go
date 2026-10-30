package api

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"

	"sklad/server/internal/auth"
)

// ---------------------------------------------------------------------------
// CRUD, filters and pagination
// ---------------------------------------------------------------------------

func TestItemsCRUDAndTagAllocation(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	env.seedOptions()

	camera := env.createItem(map[string]any{
		"name": "Basler acA5472", "sn": "SN-1", "type": "Camera", "status": "Feasibility",
	})
	if camera.AssetTag != "PV-001" {
		t.Fatalf("first auto tag should be PV-001, got %q", camera.AssetTag)
	}
	if camera.Version != 1 || camera.Archived {
		t.Fatalf("a new item starts at version 1 and is not archived: %+v", camera)
	}
	if camera.Type == nil || *camera.Type != "Camera" || camera.Status == nil || *camera.Status != "Feasibility" {
		t.Fatalf("type/status not stored: %+v", camera)
	}
	if camera.CreatedBy == nil || *camera.CreatedBy != testAdminUser {
		t.Fatalf("created_by missing: %+v", camera.CreatedBy)
	}
	if camera.UpdatedBy == nil || *camera.UpdatedBy != testAdminUser {
		t.Fatalf("updated_by missing: %+v", camera.UpdatedBy)
	}

	// An explicit tag is kept verbatim, and the next auto tag skips over it.
	explicit := env.createItem(map[string]any{"asset_tag": "PV-900", "name": "Manual tag"})
	if explicit.AssetTag != "PV-900" {
		t.Fatalf("explicit tag lost: %q", explicit.AssetTag)
	}
	next := env.createItem(map[string]any{"name": "After explicit"})
	if next.AssetTag != "PV-901" {
		t.Fatalf("auto tags follow the highest number (PV-901), got %q", next.AssetTag)
	}

	// Duplicates are refused, case-insensitively.
	status, body := env.admin.do(http.MethodPost, "/api/v1/items", map[string]any{"asset_tag": "pv-001", "name": "dup"})
	if status != http.StatusConflict || errorCode(t, body) != codeConflict {
		t.Fatalf("duplicate tag: expected 409 %s, got %d (%s)", codeConflict, status, body)
	}

	// Get one / missing / malformed id.
	if fetched := env.getItem(camera.ID); fetched.AssetTag != "PV-001" || fetched.Name != "Basler acA5472" {
		t.Fatalf("unexpected item: %+v", fetched)
	}
	if status, _ := env.admin.do(http.MethodGet, "/api/v1/items/9999", nil); status != http.StatusNotFound {
		t.Fatalf("missing item: expected 404, got %d", status)
	}
	if status, _ := env.admin.do(http.MethodGet, "/api/v1/items/abc", nil); status != http.StatusBadRequest {
		t.Fatalf("malformed id: expected 400, got %d", status)
	}

	// Labels follow the requested locale.
	payload := env.admin.mustDo(http.MethodGet, itemPath(camera.ID)+"?lang=cs", nil, http.StatusOK)
	localized := decode[itemEnvelope](t, payload).Data
	if localized.TypeLabel == nil || *localized.TypeLabel != "Kamera" {
		t.Fatalf("expected the Czech type label, got %v", localized.TypeLabel)
	}
	if localized.StatusLabel == nil || *localized.StatusLabel != "Feasibility" {
		t.Fatalf("expected the language-independent status name, got %v", localized.StatusLabel)
	}
	if localized.StatusColor == nil || *localized.StatusColor != "#93c47d" {
		t.Fatalf("status colour missing: %v", localized.StatusColor)
	}

	// Three successful creates; the refused duplicate must not be audited.
	if got := env.auditCount("create", "item"); got != 3 {
		t.Fatalf("expected 3 create audit rows, got %d", got)
	}
}

func TestItemsListSearchFilterPagination(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	env.seedOptions()

	env.createItem(map[string]any{"name": "Basler acA5472", "sn": "SN-1", "type": "Camera", "status": "Feasibility"})
	env.createItem(map[string]any{"name": "Nortech 16mm", "sn": "SN-2", "type": "Lens", "status": "Loaned", "note": "on site"})
	env.createItem(map[string]any{"asset_tag": "PV-777", "name": "Spare", "type": "Lens"})

	// Default listing: everything, default page size, meta present.
	list := env.listItems("")
	if list.Meta.Total != 3 || len(list.Data) != 3 {
		t.Fatalf("expected 3 items, got total=%d len=%d", list.Meta.Total, len(list.Data))
	}
	if list.Meta.Page != 1 || list.Meta.PerPage != defaultPerPage || list.Meta.TotalPages != 1 {
		t.Fatalf("unexpected meta: %+v", list.Meta)
	}

	// Pagination, newest state preserved across pages.
	page1 := env.listItems("?per_page=2&page=1")
	page2 := env.listItems("?per_page=2&page=2")
	if len(page1.Data) != 2 || len(page2.Data) != 1 || page2.Meta.TotalPages != 2 || page2.Meta.Page != 2 {
		t.Fatalf("pagination wrong: page1=%d page2=%d meta=%+v", len(page1.Data), len(page2.Data), page2.Meta)
	}
	seen := map[string]bool{}
	for _, item := range append(page1.Data, page2.Data...) {
		if seen[item.AssetTag] {
			t.Fatalf("tag %s appeared on both pages", item.AssetTag)
		}
		seen[item.AssetTag] = true
	}

	// Free-text search covers asset tag, name, SN and note.
	for query, want := range map[string]int{"basler": 1, "SN-2": 1, "on%20site": 1, "pv-777": 1, "nothing-matches": 0} {
		if got := env.listItems("?q=" + query).Meta.Total; got != want {
			t.Fatalf("search %q: expected %d hits, got %d", query, want, got)
		}
	}

	// Type and status filters are case-insensitive.
	if got := env.listItems("?type=camera").Meta.Total; got != 1 {
		t.Fatalf("type filter: expected 1, got %d", got)
	}
	if got := env.listItems("?status=LOANED").Meta.Total; got != 1 {
		t.Fatalf("status filter: expected 1, got %d", got)
	}
	if got := env.listItems("?type=Lens&status=Loaned").Meta.Total; got != 1 {
		t.Fatalf("combined filter: expected 1, got %d", got)
	}

	// Sorting is validated and ordered.
	sorted := env.listItems("?sort=name&order=desc")
	if len(sorted.Data) != 3 {
		t.Fatalf("sorted list: %d items", len(sorted.Data))
	}
	for _, bad := range []string{"?sort=bogus", "?order=sideways", "?per_page=0", "?per_page=1001", "?page=0", "?archived=maybe", "?overdue=perhaps"} {
		status, body := env.admin.do(http.MethodGet, "/api/v1/items"+bad, nil)
		if status != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d (%s)", bad, status, body)
		}
		if code := errorCode(t, body); code != CodeValidationFailed {
			t.Fatalf("%s: expected %s, got %s", bad, CodeValidationFailed, code)
		}
	}

	// An empty result set serialises as [] rather than null.
	empty := env.listItems("?q=does-not-exist")
	if empty.Data == nil {
		t.Fatal("empty list should serialise as an empty array")
	}
	if empty.Meta.TotalPages != 0 {
		t.Fatalf("empty list should report 0 pages, got %d", empty.Meta.TotalPages)
	}
}

func TestItemsOverdueFilterUsesTheInjectedClock(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	env.seedOptions()

	env.createItem(map[string]any{"name": "Overdue", "return_date": "2026-03-10"})
	env.createItem(map[string]any{"name": "Due today", "return_date": "2026-03-15"})
	env.createItem(map[string]any{"name": "Future", "return_date": "2026-04-01"})
	env.createItem(map[string]any{"name": "No dates"})
	archived := env.createItem(map[string]any{"name": "Archived overdue", "return_date": "2026-03-01"})
	env.admin.mustDo(http.MethodDelete, itemPath(archived.ID), nil, http.StatusOK)

	list := env.listItems("?overdue=true")
	if list.Meta.Total != 2 {
		t.Fatalf("expected 2 overdue items (10th and 15th), got %d", list.Meta.Total)
	}
	tags := []string{list.Data[0].Name, list.Data[1].Name}
	sort.Strings(tags)
	if strings.Join(tags, ",") != "Due today,Overdue" {
		t.Fatalf("unexpected overdue set: %v", tags)
	}
}

// ---------------------------------------------------------------------------
// Update semantics and optimistic concurrency
// ---------------------------------------------------------------------------

func TestItemPutReplacesAndPatchMerges(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	env.seedOptions()

	item := env.createItem(map[string]any{
		"name": "Original", "sn": "SN-1", "type": "Camera", "status": "Loaned",
		"sent_date": "2026-03-01", "return_date": "2026-04-01", "note": "keep me",
	})

	// PUT without a version is refused.
	status, body := env.admin.do(http.MethodPut, itemPath(item.ID), map[string]any{"name": "no version"})
	if status != http.StatusBadRequest || errorCode(t, body) != CodeValidationFailed {
		t.Fatalf("PUT without version: expected 400, got %d (%s)", status, body)
	}
	if status, _ := env.admin.do(http.MethodPut, itemPath(item.ID), map[string]any{"version": 0, "name": "zero"}); status != http.StatusBadRequest {
		t.Fatalf("PUT with version 0: expected 400, got %d", status)
	}

	// PUT is a replacement: omitted fields are reset.
	replaced := decode[itemEnvelope](t, env.admin.mustDo(http.MethodPut, itemPath(item.ID), map[string]any{
		"version": item.Version, "name": "Replaced", "sn": "SN-9", "status": "Loaned", "personnel_id": 2,
	}, http.StatusOK)).Data
	if replaced.Version != item.Version+1 {
		t.Fatalf("PUT did not bump the version: %d -> %d", item.Version, replaced.Version)
	}
	if replaced.Type != nil || replaced.Status == nil || replaced.SentDate != nil || replaced.ReturnDate != nil {
		t.Fatalf("PUT must clear omitted fields: %+v", replaced)
	}
	if replaced.Note != "" {
		t.Fatalf("PUT must clear the note, got %q", replaced.Note)
	}
	if replaced.Name != "Replaced" || replaced.SN != "SN-9" {
		t.Fatalf("PUT lost the provided fields: %+v", replaced)
	}

	// PATCH is a merge: only the given fields change.
	patched := decode[itemEnvelope](t, env.admin.mustDo(http.MethodPatch, itemPath(item.ID), map[string]any{
		"version": replaced.Version, "status": "Loaned", "sent_date": "2026-03-02",
	}, http.StatusOK)).Data
	if patched.Name != "Replaced" || patched.SN != "SN-9" {
		t.Fatalf("PATCH must keep unmentioned fields: %+v", patched)
	}
	if patched.Status == nil || *patched.Status != "Loaned" {
		t.Fatalf("PATCH did not set the status: %+v", patched.Status)
	}
	if patched.SentDate == nil || *patched.SentDate != "2026-03-02" {
		t.Fatalf("PATCH did not set the sent date: %v", patched.SentDate)
	}
	if patched.ReturnDate != nil {
		t.Fatalf("PATCH must leave the return date untouched, got %v", patched.ReturnDate)
	}

	// Clearing a date with an empty string leaves the other one alone.
	cleared := decode[itemEnvelope](t, env.admin.mustDo(http.MethodPatch, itemPath(item.ID), map[string]any{
		"version": patched.Version, "sent_date": "",
	}, http.StatusOK)).Data
	if cleared.SentDate != nil || cleared.Status == nil {
		t.Fatalf("clearing sent_date must not touch anything else: %+v", cleared)
	}

	// A patch that changes nothing is rejected rather than silently bumping.
	status, body = env.admin.do(http.MethodPatch, itemPath(item.ID), map[string]any{"version": cleared.Version})
	if status != http.StatusBadRequest {
		t.Fatalf("empty patch: expected 400, got %d (%s)", status, body)
	}

	if got := env.auditCount("update", "item"); got != 3 {
		t.Fatalf("expected 3 update audit rows, got %d", got)
	}
}

func TestItemOptimisticConcurrencyReturns409(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	env.seedOptions()

	item := env.createItem(map[string]any{"name": "Contended", "status": "Feasibility"})
	updated := decode[itemEnvelope](t, env.admin.mustDo(http.MethodPut, itemPath(item.ID), map[string]any{
		"version": item.Version, "name": "Winner", "status": "Feasibility",
	}, http.StatusOK)).Data

	// A stale version is refused with the current version in the details.
	status, body := env.admin.do(http.MethodPatch, itemPath(item.ID), map[string]any{
		"version": item.Version, "note": "stale write",
	})
	if status != http.StatusConflict {
		t.Fatalf("stale patch: expected 409, got %d (%s)", status, body)
	}
	if code := errorCode(t, body); code != CodeConflict {
		t.Fatalf("expected error code %s, got %s", CodeConflict, code)
	}
	if details := errorDetails(t, body); len(details) == 0 ||
		!strings.Contains(details[0], "expected version") || !strings.Contains(details[0], "current version") {
		t.Fatalf("409 details should name both versions, got %v", details)
	}

	// The refused write must not have changed anything.
	after := env.getItem(item.ID)
	if after.Name != "Winner" || after.Note != "" || after.Version != updated.Version {
		t.Fatalf("a conflicting write leaked into the row: %+v", after)
	}
	if got := env.auditCount("update", "item"); got != 1 {
		t.Fatalf("expected exactly one update audit row (the winner), got %d", got)
	}
}

// TestItemConcurrentUpdatesLoseNoWrites is the strongest statement of the
// optimistic-concurrency contract: with N writers holding the same version,
// exactly one wins, the rest get 409, and the row advances by exactly one.
func TestItemConcurrentUpdatesLoseNoWrites(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	env.seedOptions()

	item := env.createItem(map[string]any{"name": "Contended", "status": "Loaned"})

	const writers = 6
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		statuses []int
	)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			status, _, _, err := env.admin.tryDo(http.MethodPatch, itemPath(item.ID), map[string]any{
				"version": item.Version,
				"note":    fmt.Sprintf("writer-%d", index),
			})
			if err != nil {
				t.Errorf("writer %d: %v", index, err)
				return
			}
			mu.Lock()
			statuses = append(statuses, status)
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	wins, conflicts := 0, 0
	for _, status := range statuses {
		switch status {
		case http.StatusOK:
			wins++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("unexpected status %d", status)
		}
	}
	if wins != 1 || conflicts != writers-1 {
		t.Fatalf("expected 1 winner and %d conflicts, got %d/%d", writers-1, wins, conflicts)
	}
	if final := env.getItem(item.ID); final.Version != item.Version+1 {
		t.Fatalf("lost update: version is %d, want %d", final.Version, item.Version+1)
	}
	if got := env.auditCount("update", "item"); got != 1 {
		t.Fatalf("expected 1 update audit row, got %d", got)
	}
}

// TestItemConcurrentCreatesProduceUniqueTags covers the read-max-then-insert
// allocation of asset tags.
func TestItemConcurrentCreatesProduceUniqueTags(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	env.seedOptions()

	const writers = 8
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		tags []string
	)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			status, _, payload, err := env.admin.tryDo(http.MethodPost, "/api/v1/items",
				map[string]any{"name": fmt.Sprintf("concurrent-%d", index)})
			if err != nil {
				t.Errorf("writer %d: %v", index, err)
				return
			}
			if status != http.StatusCreated {
				t.Errorf("writer %d: status %d (%s)", index, status, payload)
				return
			}
			var envelope itemEnvelope
			if err := json.Unmarshal(payload, &envelope); err != nil {
				t.Errorf("writer %d: decode: %v", index, err)
				return
			}
			mu.Lock()
			tags = append(tags, envelope.Data.AssetTag)
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	if len(tags) != writers {
		t.Fatalf("expected %d created tags, got %d", writers, len(tags))
	}
	unique := map[string]bool{}
	for _, tag := range tags {
		if unique[tag] {
			t.Fatalf("tag %s was allocated twice", tag)
		}
		unique[tag] = true
	}
	if duplicates := env.countQuery(
		`SELECT COUNT(*) FROM (SELECT asset_tag FROM items GROUP BY asset_tag HAVING COUNT(*) > 1)`); duplicates != 0 {
		t.Fatalf("database holds %d duplicated asset tags", duplicates)
	}
}

// ---------------------------------------------------------------------------
// Validation and archiving
// ---------------------------------------------------------------------------

func TestItemValidation(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	env.seedOptions()

	for name, body := range map[string]map[string]any{
		"bad date":          {"sent_date": "01/02/2026"},
		"impossible date":   {"return_date": "2026-02-31"},
		"unknown type":      {"type": "Nonexistent"},
		"unknown status":    {"status": "Nonexistent"},
		"long name":         {"name": strings.Repeat("x", maxNameLen+1)},
		"long sn":           {"sn": strings.Repeat("x", maxSNLen+1)},
		"long note":         {"note": strings.Repeat("x", maxNoteLen+1)},
		"tag with space":    {"asset_tag": "PV 001"},
		"tag with newline":  {"asset_tag": "PV-00\n1"},
		"tag too long":      {"asset_tag": strings.Repeat("9", maxAssetTagLen+1)},
		"version on create": {"version": 3},
	} {
		status, payload := env.admin.do(http.MethodPost, "/api/v1/items", body)
		if status != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d (%s)", name, status, payload)
		}
		if code := errorCode(t, payload); code != CodeValidationFailed {
			t.Fatalf("%s: expected %s, got %s", name, CodeValidationFailed, code)
		}
	}

	// Surrounding whitespace is trimmed rather than rejected (scanners do this).
	item := env.createItem(map[string]any{"asset_tag": "  PV-900\t", "name": "Trimmed"})
	if item.AssetTag != "PV-900" {
		t.Fatalf("explicit tag should be trimmed, got %q", item.AssetTag)
	}

	// Validate that a rejected create did not consume an asset tag.
	next := env.createItem(map[string]any{"name": "Next"})
	if next.AssetTag != "PV-901" {
		t.Fatalf("failed creates must not consume tags, got %q", next.AssetTag)
	}
}

func TestItemArchiveIsSoftAndIdempotent(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	env.seedOptions()

	item := env.createItem(map[string]any{"name": "Archive me"})
	payload := env.admin.mustDo(http.MethodDelete, itemPath(item.ID), nil, http.StatusOK)
	if decoded := decode[map[string]any](t, payload); decoded["data"].(map[string]any)["ok"] != true {
		t.Fatalf("DELETE should answer {data:{ok:true}}, got %s", payload)
	}

	if env.listItems("").Meta.Total != 0 {
		t.Fatal("an archived item must not appear in the default list")
	}
	archived := env.listItems("?archived=true")
	if archived.Meta.Total != 1 || !archived.Data[0].Archived {
		t.Fatalf("archived item missing from the archive view: %+v", archived)
	}

	// Archiving bumps the version, so an in-flight edit with the old version
	// fails instead of resurrecting the row.
	if status, _ := env.admin.do(http.MethodPatch, itemPath(item.ID), map[string]any{
		"version": item.Version, "note": "zombie",
	}); status != http.StatusConflict {
		t.Fatalf("editing an archived item with a stale version: expected 409, got %d", status)
	}
	// Archived items stay readable.
	if fetched := env.getItem(item.ID); !fetched.Archived || fetched.Version != item.Version+1 {
		t.Fatalf("archived item should be readable with a bumped version: %+v", fetched)
	}

	// A second DELETE is a no-op and adds no audit noise.
	env.admin.mustDo(http.MethodDelete, itemPath(item.ID), nil, http.StatusOK)
	if got := env.auditCount("delete", "item"); got != 1 {
		t.Fatalf("expected exactly one delete audit row, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Scan resolution
// ---------------------------------------------------------------------------

func TestItemResolveNormalisesAndDedupes(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	item := env.createItem(map[string]any{"asset_tag": "PV-001", "name": "Scannable"})

	payload := env.admin.mustDo(http.MethodPost, "/api/v1/items/resolve", map[string]any{
		"codes": []string{" pv-001 ", "PV-001", "unknown-9", ""},
	}, http.StatusOK)
	resolved := decode[struct {
		Data struct {
			Matched []Item   `json:"matched"`
			Unknown []string `json:"unknown"`
		} `json:"data"`
	}](t, payload)

	if len(resolved.Data.Matched) != 1 || resolved.Data.Matched[0].ID != item.ID {
		t.Fatalf("expected exactly one match, got %+v", resolved.Data.Matched)
	}
	if len(resolved.Data.Unknown) != 1 || resolved.Data.Unknown[0] != "UNKNOWN-9" {
		t.Fatalf("expected the normalised unknown code, got %v", resolved.Data.Unknown)
	}

	// Scan telemetry: one row per unique code, matched rows carry the item id.
	if got := env.countQuery(`SELECT COUNT(*) FROM scan_events`); got != 2 {
		t.Fatalf("expected 2 scan_events rows (duplicates and blanks collapsed), got %d", got)
	}
	if got := env.countQuery(`SELECT COUNT(*) FROM scan_events WHERE result = 'matched' AND item_id = ?`, item.ID); got != 1 {
		t.Fatalf("expected one matched scan event pointing at the item, got %d", got)
	}
	if got := env.countQuery(`SELECT COUNT(*) FROM scan_events WHERE result = 'unknown' AND item_id IS NULL`); got != 1 {
		t.Fatalf("expected one unknown scan event without an item id, got %d", got)
	}

	// Input caps keep a hostile payload from building an unbounded query.
	if status, _ := env.admin.do(http.MethodPost, "/api/v1/items/resolve", map[string]any{"codes": []string{}}); status != http.StatusBadRequest {
		t.Fatalf("empty codes: expected 400, got %d", status)
	}
	codes := make([]string, maxResolveCodes+1)
	for i := range codes {
		codes[i] = fmt.Sprintf("PV-%04d", i)
	}
	if status, _ := env.admin.do(http.MethodPost, "/api/v1/items/resolve", map[string]any{"codes": codes}); status != http.StatusBadRequest {
		t.Fatalf("more than %d codes: expected 400, got %d", maxResolveCodes, status)
	}
}

// ---------------------------------------------------------------------------
// Bulk updates
// ---------------------------------------------------------------------------

func TestBulkUpdateAppliesAndAudits(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	env.seedOptions()

	items := []Item{
		env.createItem(map[string]any{"name": "A", "status": "Feasibility"}),
		env.createItem(map[string]any{"name": "B", "status": "Feasibility"}),
		env.createItem(map[string]any{"name": "C", "status": "Feasibility"}),
	}

	refs := make([]map[string]any, 0, len(items))
	for _, item := range items {
		refs = append(refs, map[string]any{"asset_tag": item.AssetTag, "version": item.Version})
	}
	payload := env.admin.mustDo(http.MethodPost, "/api/v1/items/bulk", map[string]any{
		"items": refs,
		"patch": map[string]any{"status": "Loaned", "personnel_id": 2, "sent_date": "2026-03-10", "return_date": "2026-05-01"},
	}, http.StatusOK)

	bulk := decode[struct {
		Data struct {
			Updated      int    `json:"updated"`
			UpdatedItems []Item `json:"updated_items"`
		} `json:"data"`
	}](t, payload)
	if bulk.Data.Updated != 3 || len(bulk.Data.UpdatedItems) != 3 {
		t.Fatalf("expected 3 updated items, got %+v", bulk.Data)
	}
	for _, item := range bulk.Data.UpdatedItems {
		if item.Status == nil || *item.Status != "Loaned" || item.SentDate == nil || item.ReturnDate == nil {
			t.Fatalf("bulk patch incomplete: %+v", item)
		}
		if item.Version != 2 {
			t.Fatalf("bulk update should bump each version once, got %d", item.Version)
		}
	}
	if got := env.auditCount("bulk_update", "item"); got != 3 {
		t.Fatalf("schema.md requires one bulk_update audit row per item, got %d", got)
	}

	// Explicit clear flags, and a patch that only touches the note.
	payload = env.admin.mustDo(http.MethodPost, "/api/v1/items/bulk", map[string]any{
		"items": []map[string]any{{"asset_tag": items[0].AssetTag, "version": 2}},
		"patch": map[string]any{"clear_sent_date": true, "clear_return_date": true, "note": "returned"},
	}, http.StatusOK)
	cleared := decode[struct {
		Data struct {
			UpdatedItems []Item `json:"updated_items"`
		} `json:"data"`
	}](t, payload)
	if item := cleared.Data.UpdatedItems[0]; item.SentDate != nil || item.ReturnDate != nil || item.Note != "returned" {
		t.Fatalf("clear flags did not work: %+v", item)
	}
}

// TestBulkUpdateIsAllOrNothing is the transactional guarantee from schema.md:
// one bad entry rolls the whole batch back.
func TestBulkUpdateIsAllOrNothing(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	env.seedOptions()

	first := env.createItem(map[string]any{"name": "First", "status": "Feasibility"})
	second := env.createItem(map[string]any{"name": "Second", "status": "Feasibility"})

	// A stale version anywhere in the batch aborts everything.
	status, body := env.admin.do(http.MethodPost, "/api/v1/items/bulk", map[string]any{
		"items": []map[string]any{
			{"asset_tag": first.AssetTag, "version": first.Version},
			{"asset_tag": second.AssetTag, "version": first.Version + 5}, // stale
		},
		"patch": map[string]any{"status": "Loaned", "personnel_id": 2, "note": "must not survive"},
	})
	if status != http.StatusConflict {
		t.Fatalf("stale batch: expected 409, got %d (%s)", status, body)
	}
	for _, item := range []Item{first, second} {
		after := env.getItem(item.ID)
		if after.Version != item.Version || after.Status == nil || *after.Status != "Feasibility" || after.Note != "" {
			t.Fatalf("%s changed despite the rollback: %+v", item.AssetTag, after)
		}
	}
	if got := env.auditCount("bulk_update", "item"); got != 0 {
		t.Fatalf("a rolled-back batch must not leave audit rows, got %d", got)
	}

	// An invalid value later in the batch rolls back too (not just conflicts).
	status, body = env.admin.do(http.MethodPost, "/api/v1/items/bulk", map[string]any{
		"items": []map[string]any{
			{"asset_tag": first.AssetTag, "version": first.Version},
			{"asset_tag": second.AssetTag, "version": second.Version},
		},
		"patch": map[string]any{"status": "Nonexistent"},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("invalid value: expected 400, got %d (%s)", status, body)
	}
	for _, item := range []Item{first, second} {
		if after := env.getItem(item.ID); after.Version != item.Version {
			t.Fatalf("%s changed despite a validation rollback: %+v", item.AssetTag, after)
		}
	}

	// A missing item aborts with 404.
	status, _ = env.admin.do(http.MethodPost, "/api/v1/items/bulk", map[string]any{
		"items": []map[string]any{
			{"asset_tag": first.AssetTag, "version": first.Version},
			{"asset_tag": "PV-777", "version": 1},
		},
		"patch": map[string]any{"status": "Loaned", "personnel_id": 2, "note": "nope"},
	})
	if status != http.StatusNotFound {
		t.Fatalf("unknown tag in a batch: expected 404, got %d", status)
	}
}

func TestBulkUpdateValidation(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	env.seedOptions()
	item := env.createItem(map[string]any{"name": "Solo", "status": "Feasibility"})

	for name, body := range map[string]map[string]any{
		"no items":        {"items": []any{}, "patch": map[string]any{"status": "Loaned"}},
		"no patch":        {"items": []map[string]any{{"asset_tag": item.AssetTag, "version": 1}}, "patch": map[string]any{}},
		"missing ref":     {"items": []map[string]any{{"version": 1}}, "patch": map[string]any{"note": "x"}},
		"missing version": {"items": []map[string]any{{"asset_tag": item.AssetTag}}, "patch": map[string]any{"note": "x"}},
		"duplicate refs": {"items": []map[string]any{
			{"asset_tag": item.AssetTag, "version": 1},
			{"asset_tag": strings.ToLower(item.AssetTag), "version": 1},
		}, "patch": map[string]any{"note": "x"}},
		"contradiction": {"items": []map[string]any{{"asset_tag": item.AssetTag, "version": 1}},
			"patch": map[string]any{"sent_date": "2026-03-01", "clear_sent_date": true}},
		"malformed tag": {"items": []map[string]any{{"asset_tag": "PV 001", "version": 1}},
			"patch": map[string]any{"note": "x"}},
	} {
		status, payload := env.admin.do(http.MethodPost, "/api/v1/items/bulk", body)
		if status != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d (%s)", name, status, payload)
		}
	}
	if got := env.auditCount("bulk_update", "item"); got != 0 {
		t.Fatalf("rejected batches must not be audited, got %d rows", got)
	}
}

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

func TestOptionsListRenameCascadeAndGuards(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	env.seedOptions()

	item := env.createItem(map[string]any{"name": "Typed", "type": "Lens", "status": "Feasibility"})

	// GET /options is public (openapi: security: []) and locale-aware.
	anon := &sessionClient{t: t, server: env.server, client: newAnonymousClient(t)}
	payload := anon.mustDo(http.MethodGet, "/api/v1/options?lang=cs", nil, http.StatusOK)
	options := decode[struct {
		Data struct {
			Types    []typeOption   `json:"types"`
			Statuses []statusOption `json:"statuses"`
		} `json:"data"`
	}](t, payload)
	if len(options.Data.Types) != 2 || len(options.Data.Statuses) != 2 {
		t.Fatalf("unexpected option counts: %d/%d", len(options.Data.Types), len(options.Data.Statuses))
	}
	if options.Data.Types[0].Label != "Kamera" || options.Data.Types[0].Labels.EN != "Camera" {
		t.Fatalf("localized labels wrong: %+v", options.Data.Types[0])
	}

	// Renaming a type cascades to the item through the foreign key.
	env.admin.mustDo(http.MethodPut, "/api/v1/options", map[string]any{
		"types": []map[string]any{
			{"value": "Camera", "labels": map[string]string{"en": "Camera", "cs": "Kamera", "it": "Telecamera"}},
			{"value": "Objective", "labels": map[string]string{"en": "Objective"}},
		},
		"statuses": []map[string]any{
			{"value": "Feasibility", "labels": map[string]string{"en": "Feasibility"}, "color": "#93c47d"},
			{"value": "Loaned", "labels": map[string]string{"en": "Loaned"}, "color": "#e67e22"},
		},
		"renames": map[string]any{"types": map[string]string{"Lens": "Objective"}},
	}, http.StatusOK)

	renamed := env.getItem(item.ID)
	if renamed.Type == nil || *renamed.Type != "Objective" {
		t.Fatalf("the rename did not reach the item: %v", renamed.Type)
	}
	if renamed.Version != item.Version {
		t.Fatalf("renaming an option must not bump the item version: %d -> %d", item.Version, renamed.Version)
	}

	// Deleting an option that items still use is refused.
	status, body := env.admin.do(http.MethodPut, "/api/v1/options", map[string]any{
		"types":    []map[string]any{{"value": "Camera", "labels": map[string]string{"en": "Camera"}}},
		"statuses": []map[string]any{{"value": "Feasibility", "labels": map[string]string{"en": "Feasibility"}}},
	})
	if status != http.StatusConflict {
		t.Fatalf("in-use option delete: expected 409, got %d (%s)", status, body)
	}
	if code := errorCode(t, body); code != codeConflict {
		t.Fatalf("expected %s, got %s", codeConflict, code)
	}

	// Removing an unused status is allowed and keeps the used one.
	payload = env.admin.mustDo(http.MethodPut, "/api/v1/options", map[string]any{
		"types": []map[string]any{
			{"value": "Camera", "labels": map[string]string{"en": "Camera"}},
			{"value": "Objective", "labels": map[string]string{"en": "Objective"}},
		},
		"statuses": []map[string]any{{"value": "Feasibility", "labels": map[string]string{"en": "Feasibility"}}},
	}, http.StatusOK)
	after := decode[struct {
		Data struct {
			Statuses []statusOption `json:"statuses"`
		} `json:"data"`
	}](t, payload)
	if len(after.Data.Statuses) != 1 {
		t.Fatalf("expected one status left, got %d", len(after.Data.Statuses))
	}
	if got := env.auditCount("options_update", "options"); got != 2 {
		t.Fatalf("expected 2 options_update audit rows, got %d", got)
	}

	// Validation of the incoming option sets.
	for name, body := range map[string]map[string]any{
		"duplicate values": {"types": []map[string]any{
			{"value": "A", "labels": map[string]string{"en": "A"}},
			{"value": "a", "labels": map[string]string{"en": "a"}},
		}, "statuses": []any{}},
		"bad colour": {"types": []map[string]any{{"value": "A", "labels": map[string]string{"en": "A"}}},
			"statuses": []map[string]any{{"value": "S", "labels": map[string]string{"en": "S"}, "color": "red"}}},
		"unknown rename": {"types": []map[string]any{{"value": "A", "labels": map[string]string{"en": "A"}}},
			"statuses": []any{}, "renames": map[string]any{"types": map[string]string{"Ghost": "A"}}},
		"missing label": {"types": []map[string]any{{"value": "A"}}, "statuses": []any{}},
	} {
		status, payload := env.admin.do(http.MethodPut, "/api/v1/options", body)
		if status != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d (%s)", name, status, payload)
		}
	}
}

// ---------------------------------------------------------------------------
// Export
// ---------------------------------------------------------------------------

func TestExportJSONAndCSV(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	env.seedOptions()

	env.createItem(map[string]any{
		"name": "Ünïcode — täg", "sn": "SN,1", "type": "Camera", "status": "Loaned", "note": "line1\nline2",
	})
	env.createItem(map[string]any{"name": "Plain", "type": "Lens"})

	// JSON export, timestamped with the injected clock.
	payload := env.admin.mustDo(http.MethodGet, "/api/v1/export?format=json", nil, http.StatusOK)
	document := decode[struct {
		Data struct {
			ExportedAt string `json:"exported_at"`
			Total      int    `json:"total"`
			Items      []Item `json:"items"`
		} `json:"data"`
	}](t, payload)
	if document.Data.Total != 2 || len(document.Data.Items) != 2 {
		t.Fatalf("expected 2 exported items, got %+v", document.Data)
	}
	if document.Data.ExportedAt != testNow.Format("2006-01-02T15:04:05Z07:00") {
		t.Fatalf("exported_at should come from the injected clock, got %q", document.Data.ExportedAt)
	}

	// CSV export: BOM, CRLF, the documented header and preserved text.
	status, header, body := env.admin.doFull(http.MethodGet, "/api/v1/export?format=csv", nil)
	if status != http.StatusOK {
		t.Fatalf("csv export: %d", status)
	}
	if ct := header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("unexpected content type %q", ct)
	}
	if disposition := header.Get("Content-Disposition"); !strings.Contains(disposition, "attachment") {
		t.Fatalf("missing attachment disposition: %q", disposition)
	}
	if !bytes.HasPrefix(body, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("CSV must start with a UTF-8 BOM so Excel detects the encoding")
	}
	if !bytes.Contains(body, []byte("\r\n")) {
		t.Fatal("CSV must use CRLF line endings")
	}
	records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected a header plus two rows, got %d", len(records))
	}
	if strings.Join(records[0], ",") != strings.Join(csvColumns, ",") {
		t.Fatalf("unexpected CSV header: %v", records[0])
	}
	if records[1][2] != "Ünïcode — täg" || records[1][8] != "line1\nline2" {
		t.Fatalf("CSV did not preserve values: %v", records[1])
	}

	// Parameter validation.
	for _, bad := range []string{"?format=xml", "?archived=nope"} {
		if status, _ := env.admin.do(http.MethodGet, "/api/v1/export"+bad, nil); status != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d", bad, status)
		}
	}
}

func TestCSVExportDefusesFormulas(t *testing.T) {
	t.Parallel()
	personnel := "@SUM(A1:A9)"
	row := csvRow(Item{
		ID: 7, AssetTag: "PV-007", Name: "=HYPERLINK(\"http://x\",\"click\")", SN: "+420",
		Note: "-2+3", PersonnelName: &personnel,
	})
	byColumn := make(map[string]string, len(csvColumns))
	for i, column := range csvColumns {
		byColumn[column] = row[i]
	}
	want := map[string]string{
		"id": "7", "asset_tag": "PV-007", "name": "'=HYPERLINK(\"http://x\",\"click\")",
		"sn": "'+420", "note": "'-2+3", "personnel_name": "'@SUM(A1:A9)",
	}
	for column, value := range want {
		if byColumn[column] != value {
			t.Errorf("%s: got %q, want %q", column, byColumn[column], value)
		}
	}
	for _, cell := range []string{"\tcmd", "\rcmd"} {
		if got := csvSafe(cell); got != "'"+cell {
			t.Errorf("csvSafe(%q) = %q", cell, got)
		}
	}
}

// ---------------------------------------------------------------------------
// RBAC, CSRF and the audit trail
// ---------------------------------------------------------------------------

func TestAPIRequiresAuthCSRFAndRoles(t *testing.T) {
	env := newTestEnv(t)
	env.seedOptions()
	item := env.createItem(map[string]any{"name": "Visible"})
	anon := &sessionClient{t: t, server: env.server, client: newAnonymousClient(t)}
	for _, path := range []string{"/api/v1/settings", "/api/v1/users", "/api/v1/notifications", "/api/v1/export"} {
		anon.mustDo(http.MethodGet, path, nil, http.StatusUnauthorized)
	}
	for _, path := range []string{"/api/v1/items", "/api/v1/options", "/api/v1/personnel"} {
		anon.mustDo(http.MethodGet, path, nil, http.StatusOK)
	}
	anon.mustDo(http.MethodPost, "/api/v1/items", map[string]any{"name": "no"}, http.StatusUnauthorized)
	anon.mustDo(http.MethodDelete, itemPath(item.ID), nil, http.StatusUnauthorized)
	anon.mustDo(http.MethodPatch, itemPath(item.ID), map[string]any{"version": item.Version, "name": "no"}, http.StatusForbidden)
	anon.mustDo(http.MethodPost, "/api/v1/personnel", map[string]any{"name": "no"}, http.StatusUnauthorized)
	noCSRF := &sessionClient{t: t, server: env.server, client: env.admin.client}
	noCSRF.mustDo(http.MethodPatch, itemPath(item.ID), map[string]any{"version": item.Version, "status": "Feasibility"}, http.StatusForbidden)
}

func TestAuditTrailRecordsBeforeAndAfter(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	env.seedOptions()

	item := env.createItem(map[string]any{"name": "Before", "status": "Feasibility"})
	var (
		before sql.NullString
		after  sql.NullString
	)
	if err := env.db.QueryRow(`SELECT before, after FROM audit_log WHERE action = 'create' AND entity_id = ?`,
		item.AssetTag).Scan(&before, &after); err != nil {
		t.Fatalf("read create audit row: %v", err)
	}
	if before.Valid {
		t.Fatalf("a create must not have a before-snapshot, got %s", before.String)
	}
	if !after.Valid || !strings.Contains(after.String, `"asset_tag":"PV-001"`) {
		t.Fatalf("create audit row should carry the new item: %v", after)
	}

	env.admin.mustDo(http.MethodPatch, itemPath(item.ID), map[string]any{
		"version": item.Version, "name": "After",
	}, http.StatusOK)
	if err := env.db.QueryRow(`SELECT before, after FROM audit_log WHERE action = 'update' AND entity_id = ?`,
		item.AssetTag).Scan(&before, &after); err != nil {
		t.Fatalf("read update audit row: %v", err)
	}
	if !before.Valid || !strings.Contains(before.String, `"name":"Before"`) {
		t.Fatalf("update audit row should carry the previous state: %v", before)
	}
	if !after.Valid || !strings.Contains(after.String, `"name":"After"`) || !strings.Contains(after.String, `"version":2`) {
		t.Fatalf("update audit row should carry the new state: %v", after)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newAnonymousClient returns a cookie-less client for the public endpoints.
func newAnonymousClient(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{}
}

// createUserAs inserts a user directly (the harness has no user API) and
// returns a logged-in client for it.
func (e *testEnv) createUserAs(t *testing.T, username, password, role string) *sessionClient {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	e.exec(`INSERT INTO users (username, display_name, email, password_hash, role, must_change_password, active, created_at, updated_at)
		VALUES (?, ?, '', ?, ?, 0, 1, '2026-03-01T00:00:00Z', '2026-03-01T00:00:00Z')`,
		username, username, hash, role)
	return e.loginAs(username, password)
}

func TestWorkspaceHasOnlyAdminAccount(t *testing.T) {
	env := newTestEnv(t)
	for _, role := range []string{"viewer", "editor", "admin"} {
		env.admin.mustDo(http.MethodPost, "/api/v1/users", map[string]any{"username": role, "role": role, "password": "test-password-2"}, http.StatusForbidden)
	}
	hash, err := auth.HashPassword("legacy-password-1")
	if err != nil {
		t.Fatal(err)
	}
	env.exec(`INSERT INTO users(username,password_hash,role,created_at,updated_at) VALUES('editor',?,'editor','2026-03-01','2026-03-01')`, hash)
	anon := &sessionClient{t: t, server: env.server, client: newAnonymousClient(t)}
	anon.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "editor", "password": "legacy-password-1"}, http.StatusUnauthorized)
}

func TestPublicCannotReadArchivedEquipment(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	item := env.createItem(map[string]any{"name": "Archived staff record"})
	env.admin.mustDo(http.MethodDelete, fmt.Sprintf("/api/v1/items/%d", item.ID), nil, http.StatusOK)
	anon := &sessionClient{t: t, server: env.server, client: newAnonymousClient(t)}
	for path, want := range map[string]int{
		"/api/v1/items?archived=true":            http.StatusForbidden,
		fmt.Sprintf("/api/v1/items/%d", item.ID): http.StatusNotFound,
		"/api/v1/export?archived=true":           http.StatusUnauthorized,
	} {
		if status, body := anon.do(http.MethodGet, path, nil); status != want {
			t.Fatalf("public %s: got %d, want %d: %s", path, status, want, body)
		}
	}
	payload := anon.mustDo(http.MethodPost, "/api/v1/items/resolve", map[string]any{"codes": []string{item.AssetTag}}, http.StatusOK)
	result := decode[struct {
		Data struct {
			Matched []Item   `json:"matched"`
			Unknown []string `json:"unknown"`
		} `json:"data"`
	}](t, payload)
	if len(result.Data.Matched) != 0 || len(result.Data.Unknown) != 1 {
		t.Fatalf("archived scan leaked: %s", payload)
	}
	// Authenticated staff can still read archives and export them.
	for _, path := range []string{"/api/v1/items?archived=true", fmt.Sprintf("/api/v1/items/%d", item.ID), "/api/v1/export?archived=true"} {
		if status, body := env.admin.do(http.MethodGet, path, nil); status != http.StatusOK {
			t.Fatalf("staff %s: %d %s", path, status, body)
		}
	}
}
