package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestRentalWorkflow(t *testing.T) {
	env := newTestEnv(t)
	env.seedOptions()
	anon := &sessionClient{t: t, server: env.server, client: newAnonymousClient(t)}
	person := decode[struct {
		Data Person `json:"data"`
	}](t, env.admin.mustDo(http.MethodPost, "/api/v1/personnel", map[string]any{"name": "Office colleague"}, 201)).Data
	item := decode[itemEnvelope](t, env.admin.mustDo(http.MethodPost, "/api/v1/items", map[string]any{"name": "Device"}, 201)).Data
	if item.Status == nil || *item.Status != "Feasibility" {
		t.Fatal("default must be Feasibility")
	}
	patch := func(ch map[string]any, want int) []byte {
		ch["version"] = item.Version
		return anon.mustDo(http.MethodPatch, itemPath(item.ID), ch, want)
	}
	for _, ch := range []map[string]any{{"note": "locked"}, {"sent_date": "2026-10-15"}, {"return_date": "2026-10-20"}, {"personnel_id": person.ID}} {
		patch(ch, 400)
	}
	patch(map[string]any{"status": "Loaned"}, 400)
	patch(map[string]any{"status": "Loaned", "personnel_id": 1, "note": "  \n "}, 400)
	for _, key := range []string{"name", "sn", "type", "asset_tag", "archived", "created_by"} {
		patch(map[string]any{key: nil}, 403)
	}
	item = decode[itemEnvelope](t, patch(map[string]any{"status": "Loaned", "personnel_id": person.ID, "sent_date": "2026-10-15", "return_date": "2026-10-20", "note": "Office test"}, 200)).Data
	if item.PersonnelID == nil || *item.PersonnelID != person.ID {
		t.Fatal("person not persisted")
	}
	patch(map[string]any{"status": "Feasibility"}, 400)
	item = decode[itemEnvelope](t, patch(map[string]any{"personnel_id": 1, "note": "ACME customer"}, 200)).Data
	patch(map[string]any{"note": "   "}, 400)
	// All Admin write paths enforce Customer validation too.
	env.admin.mustDo(http.MethodPut, itemPath(item.ID), map[string]any{"version": item.Version, "status": "Loaned", "personnel_id": 1, "note": " "}, 400)
	item = decode[itemEnvelope](t, env.admin.mustDo(http.MethodPatch, itemPath(item.ID), map[string]any{"version": item.Version, "status": "Feasibility"}, 200)).Data
	if item.Note != "ACME customer" {
		t.Fatal("return should preserve historical rental fields")
	}
	patch(map[string]any{"note": "change while returned"}, 400)
	item = decode[itemEnvelope](t, env.admin.mustDo(http.MethodPut, itemPath(item.ID), map[string]any{"version": item.Version, "name": "Device", "status": "Feasibility"}, 200)).Data
	if item.Note != "ACME customer" {
		t.Fatal("Feasibility replacement lost rental history")
	}
	// Deactivation preserves historical references but prevents new assignments.
	other := env.createItem(map[string]any{"name": "Other", "status": "Loaned", "personnel_id": person.ID})
	person = decode[struct {
		Data Person `json:"data"`
	}](t, env.admin.mustDo(http.MethodPatch, fmt.Sprintf("/api/v1/personnel/%d", person.ID), map[string]any{"active": false, "version": person.Version}, 200)).Data
	patch(map[string]any{"status": "Loaned", "personnel_id": person.ID}, 400)
	anon.mustDo(http.MethodPatch, itemPath(other.ID), map[string]any{"version": other.Version, "note": "Existing rental still valid"}, 200)
	if fresh := env.getItem(other.ID); fresh.PersonnelID == nil || *fresh.PersonnelID != person.ID {
		t.Fatal("deactivation lost reference")
	}
	env.admin.mustDo(http.MethodPatch, "/api/v1/personnel/1", map[string]any{"active": false, "version": 1}, 400)
	anon.mustDo(http.MethodPatch, fmt.Sprintf("/api/v1/personnel/%d", person.ID), map[string]any{"name": "tampered", "version": person.Version}, 401)
}

func TestRentalBulkAtomicValidation(t *testing.T) {
	env := newTestEnv(t)
	env.seedOptions()
	anon := &sessionClient{t: t, server: env.server, client: newAnonymousClient(t)}
	a := env.createItem(map[string]any{"name": "A"})
	b := env.createItem(map[string]any{"name": "B"})
	refs := []map[string]any{{"asset_tag": a.AssetTag, "version": a.Version}, {"asset_tag": b.AssetTag, "version": b.Version}}
	for _, patch := range []map[string]any{{"status": "Loaned"}, {"status": "Loaned", "personnel_id": 1, "note": " "}, {"note": "locked"}} {
		response := anon.mustDo(http.MethodPost, "/api/v1/items/bulk", map[string]any{"items": refs, "patch": patch}, 400)
		if !strings.Contains(string(response), a.AssetTag) {
			t.Fatalf("missing item identification: %s", response)
		}
		if env.getItem(a.ID).Version != a.Version || env.getItem(b.ID).Version != b.Version {
			t.Fatal("invalid batch partially committed")
		}
	}
	anon.mustDo(http.MethodPost, "/api/v1/items/bulk", map[string]any{"items": refs, "patch": map[string]any{"status": "Loaned", "personnel_id": 1, "note": "ACME"}}, 200)
	a = env.getItem(a.ID)
	b = env.getItem(b.ID)
	refs = []map[string]any{{"asset_tag": a.AssetTag, "version": a.Version}, {"asset_tag": b.AssetTag, "version": b.Version - 1}}
	anon.mustDo(http.MethodPost, "/api/v1/items/bulk", map[string]any{"items": refs, "patch": map[string]any{"note": "new"}}, 409)
	if env.getItem(a.ID).Note != "ACME" {
		t.Fatal("conflict partially committed")
	}
	refs[1]["version"] = b.Version
	anon.mustDo(http.MethodPost, "/api/v1/items/bulk", map[string]any{"items": refs, "patch": map[string]any{"status": "Feasibility"}}, 400)
	env.admin.mustDo(http.MethodPost, "/api/v1/items/bulk", map[string]any{"items": refs, "patch": map[string]any{"status": "Feasibility"}}, 200)
}

func TestReturnedItemExcludedFromOverdueInventory(t *testing.T) {
	env := newTestEnv(t)
	env.seedOptions()
	item := decode[itemEnvelope](t, env.admin.mustDo(http.MethodPost, "/api/v1/items", map[string]any{"name": "Returned", "status": "Loaned", "personnel_id": 1, "note": "Customer ACME", "return_date": "2000-01-01"}, 201)).Data
	env.admin.mustDo(http.MethodPatch, itemPath(item.ID), map[string]any{"version": item.Version, "status": "Feasibility"}, 200)
	result := decode[struct{ Data []Item }](t, env.admin.mustDo(http.MethodGet, "/api/v1/items?overdue=true", nil, 200))
	for _, got := range result.Data {
		if got.ID == item.ID {
			t.Fatal("returned item still overdue")
		}
	}
	saved := decode[itemEnvelope](t, env.admin.mustDo(http.MethodGet, itemPath(item.ID), nil, 200)).Data
	if saved.ReturnDate == nil || *saved.ReturnDate != "2000-01-01" {
		t.Fatal("return history lost")
	}
}

func TestSharedStatusNamesInEveryLocale(t *testing.T) {
	for _, locale := range []string{"en", "cs", "it"} {
		rows := []optionRow{}
		for _, value := range []string{"Feasibility", "Devel", "Sales"} {
			rows = append(rows, optionRow{Value: value, Labels: localizedLabels{EN: "translated", CS: "translated", IT: "translated"}})
		}
		got := optionsResponse(nil, rows, locale)["statuses"].([]statusOption)
		for _, status := range got {
			if status.Label != status.Value || status.Labels.CS != status.Value || status.Labels.IT != status.Value {
				t.Fatalf("%s: %+v", locale, status)
			}
		}
	}
}
