package api

import (
	"net/http"
	"sync"
	"testing"

	"sklad/server/internal/jobs"
)

// recordingAnnouncer captures what the handlers hand to the rental announcer.
type recordingAnnouncer struct {
	mu    sync.Mutex
	calls []announcement
}

type announcement struct {
	changes []jobs.RentalChange
	actor   string
}

func (r *recordingAnnouncer) AnnounceRentals(changes []jobs.RentalChange, actor string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, announcement{changes: append([]jobs.RentalChange(nil), changes...), actor: actor})
}

// take returns and clears the recorded calls.
func (r *recordingAnnouncer) take() []announcement {
	r.mu.Lock()
	defer r.mu.Unlock()
	calls := r.calls
	r.calls = nil
	return calls
}

func TestRentalChangesAreAnnounced(t *testing.T) {
	announcer := &recordingAnnouncer{}
	env := newTestEnv(t, withRentals(announcer))
	env.seedOptions()
	anon := &sessionClient{t: t, server: env.server, client: newAnonymousClient(t)}
	person := decode[struct {
		Data Person `json:"data"`
	}](t, env.admin.mustDo(http.MethodPost, "/api/v1/personnel", map[string]any{"name": "Office colleague"}, 201)).Data
	item := decode[itemEnvelope](t, env.admin.mustDo(http.MethodPost, "/api/v1/items", map[string]any{"name": "Lens"}, 201)).Data
	if calls := announcer.take(); len(calls) != 0 {
		t.Fatalf("creating an item in Feasibility must not be announced: %+v", calls)
	}

	// Feasibility → Loaned by the anonymous Editor: one change, full details.
	item = decode[itemEnvelope](t, anon.mustDo(http.MethodPatch, itemPath(item.ID), map[string]any{
		"version": item.Version, "status": "Loaned", "personnel_id": person.ID,
		"sent_date": "2026-03-15", "return_date": "2026-03-20", "note": "Trade fair",
	}, 200)).Data
	calls := announcer.take()
	if len(calls) != 1 || len(calls[0].changes) != 1 {
		t.Fatalf("expected one announcement with one change, got %+v", calls)
	}
	got := calls[0].changes[0]
	want := jobs.RentalChange{
		AssetTag: item.AssetTag, Name: "Lens", PreviousStatus: "Feasibility", Status: "Loaned",
		Renter: "Office colleague", SentDate: "2026-03-15", ReturnDate: "2026-03-20", Note: "Trade fair",
	}
	if got.AssetTag != want.AssetTag || got.Name != want.Name || got.PreviousStatus != want.PreviousStatus ||
		got.Status != want.Status || got.Renter != want.Renter || got.SentDate != want.SentDate ||
		got.ReturnDate != want.ReturnDate || got.Note != want.Note {
		t.Fatalf("rental change\n got  %+v\n want %+v", got, want)
	}
	if calls[0].actor != "" {
		t.Fatalf("the anonymous Editor has no actor name, got %q", calls[0].actor)
	}

	// Editing the note of a rented item is announced too.
	item = decode[itemEnvelope](t, anon.mustDo(http.MethodPatch, itemPath(item.ID), map[string]any{
		"version": item.Version, "note": "Trade fair, booth 4",
	}, 200)).Data
	if calls := announcer.take(); len(calls) != 1 || calls[0].changes[0].Note != "Trade fair, booth 4" ||
		calls[0].changes[0].PreviousStatus != "Loaned" {
		t.Fatalf("note edit on a rented item must be announced: %+v", calls)
	}

	// An Admin edit of the item record (not the rental) stays silent.
	item = decode[itemEnvelope](t, env.admin.mustDo(http.MethodPatch, itemPath(item.ID), map[string]any{
		"version": item.Version, "name": "Lens 50 mm",
	}, 200)).Data
	if calls := announcer.take(); len(calls) != 0 {
		t.Fatalf("renaming a rented item must not be announced: %+v", calls)
	}

	// A confirmed return (→ Feasibility) stays silent.
	env.admin.mustDo(http.MethodPatch, itemPath(item.ID), map[string]any{
		"version": item.Version, "status": "Feasibility",
	}, 200)
	if calls := announcer.take(); len(calls) != 0 {
		t.Fatalf("returns must not be announced: %+v", calls)
	}
}

func TestBulkRentalIsOneAnnouncement(t *testing.T) {
	announcer := &recordingAnnouncer{}
	env := newTestEnv(t, withRentals(announcer))
	env.seedOptions()
	a := env.createItem(map[string]any{"name": "Camera A"})
	b := env.createItem(map[string]any{"name": "Camera B"})
	refs := []map[string]any{{"asset_tag": a.AssetTag, "version": a.Version}, {"asset_tag": b.AssetTag, "version": b.Version}}

	// A rejected batch announces nothing.
	env.admin.mustDo(http.MethodPost, "/api/v1/items/bulk", map[string]any{"items": refs, "patch": map[string]any{"status": "Loaned"}}, 400)
	if calls := announcer.take(); len(calls) != 0 {
		t.Fatalf("a rejected batch must not be announced: %+v", calls)
	}

	env.admin.mustDo(http.MethodPost, "/api/v1/items/bulk", map[string]any{
		"items": refs, "patch": map[string]any{"status": "Loaned", "personnel_id": 1, "note": "ACME s.r.o."},
	}, 200)
	calls := announcer.take()
	if len(calls) != 1 || len(calls[0].changes) != 2 {
		t.Fatalf("expected one announcement with two changes, got %+v", calls)
	}
	if calls[0].actor != testAdminUser {
		t.Fatalf("an Admin save must name the Admin, got %q", calls[0].actor)
	}
	for _, change := range calls[0].changes {
		if change.Status != "Loaned" || change.PreviousStatus != "Feasibility" || change.Note != "ACME s.r.o." || change.Renter == "" {
			t.Fatalf("unexpected bulk change %+v", change)
		}
	}
}

func TestRentalAlertsSetting(t *testing.T) {
	env := newTestEnv(t)
	got := decode[struct {
		Data settingsResponse `json:"data"`
	}](t, env.admin.mustDo(http.MethodGet, "/api/v1/settings", nil, 200)).Data
	if got.Discord.RentalAlerts {
		t.Fatal("rental alerts must default to off")
	}
	got = decode[struct {
		Data settingsResponse `json:"data"`
	}](t, env.admin.mustDo(http.MethodPut, "/api/v1/settings", map[string]any{"discord": map[string]any{"rental_alerts": true}}, 200)).Data
	if !got.Discord.RentalAlerts || got.Discord.Enabled {
		t.Fatalf("rental_alerts must update on its own, got %+v", got.Discord)
	}
}
