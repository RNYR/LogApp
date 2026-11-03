package jobs

import (
	"context"
	"testing"
	"time"

	"sklad/server/internal/notify"
	"sklad/server/internal/settings"
)

func (e *jobsEnv) setRentalAlerts(enabled, rentals bool) {
	e.t.Helper()
	current, err := settings.Load(context.Background(), e.db)
	if err != nil {
		e.t.Fatalf("settings.Load: %v", err)
	}
	current.DiscordEnabled = enabled
	current.RentalAlerts = rentals
	owner := e.ownerID
	if err := settings.Save(context.Background(), e.db, current, &owner); err != nil {
		e.t.Fatalf("settings.Save: %v", err)
	}
}

var sampleRental = []RentalChange{{
	AssetTag: "PV-001", Name: "Camera A", Type: "Camera", PreviousStatus: "Feasibility", Status: "Loaned",
	StatusColor: "#e67e22", Renter: "Office colleague", SentDate: "2026-03-15", ReturnDate: "2026-03-20", Note: "Trade fair",
}}

func TestRentalAlertsNeedBothSwitches(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name             string
		enabled, rentals bool
	}{{"all off", false, false}, {"only Discord", true, false}, {"only rentals", false, true}} {
		env := newJobsEnv(t)
		env.seedSettings(testWebhook, false, "en", 11, 0)
		env.setRentalAlerts(tc.enabled, tc.rentals)
		if err := env.newScheduler(0).NotifyRentals(context.Background(), sampleRental, ""); err != nil {
			t.Fatalf("%s: NotifyRentals: %v", tc.name, err)
		}
		if env.fake.count() != 0 || len(env.deliveries()) != 0 {
			t.Fatalf("%s: nothing may be sent or logged", tc.name)
		}
	}
}

func TestRentalAlertIsSentAndLogged(t *testing.T) {
	t.Parallel()
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "en", 11, 0)
	env.setRentalAlerts(true, true)

	if err := env.newScheduler(0).NotifyRentals(context.Background(), sampleRental, "admin"); err != nil {
		t.Fatalf("NotifyRentals: %v", err)
	}
	report := env.fake.last()
	if report.Kind != notify.KindRental || report.Actor != "admin" || len(report.Items) != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	item := report.Items[0]
	if item.PreviousStatus != "Feasibility" || item.Status != "Loaned" || item.Renter != "Office colleague" || item.Note != "Trade fair" {
		t.Fatalf("unexpected item: %+v", item)
	}
	rows := env.deliveries()
	if len(rows) != 1 || rows[0].Trigger != "rental" || rows[0].Status != statusSent || rows[0].ItemCount != 1 {
		t.Fatalf("unexpected delivery log: %+v", rows)
	}

	// The same item changed again is a second, separate message.
	if err := env.newScheduler(0).NotifyRentals(context.Background(), sampleRental, ""); err != nil {
		t.Fatalf("NotifyRentals again: %v", err)
	}
	if env.fake.count() != 2 || len(env.deliveries()) != 2 {
		t.Fatalf("each save is its own message: %d sent, %d logged", env.fake.count(), len(env.deliveries()))
	}
}

func TestRentalAlertDoesNotHideOverdueReport(t *testing.T) {
	t.Parallel()
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "en", 11, 0)
	env.setRentalAlerts(true, true)
	env.insertItem("PV-001", "Camera A", "2026-03-10", false) // overdue

	scheduler := env.newScheduler(0)
	if err := scheduler.NotifyRentals(context.Background(), sampleRental, ""); err != nil {
		t.Fatalf("NotifyRentals: %v", err)
	}
	result, err := scheduler.Run(context.Background(), notify.KindOverdueDaily)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.NewCount != 1 || !result.Sent {
		t.Fatalf("a rental alert must not count as today's overdue notice: %+v", result)
	}
}

func TestAnnounceRentalsRunsInTheBackground(t *testing.T) {
	t.Parallel()
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "en", 11, 0)
	env.setRentalAlerts(true, true)
	release := make(chan struct{})
	env.fake.block = release

	scheduler := env.newScheduler(0)
	scheduler.AnnounceRentals(sampleRental, "")
	if scheduler.Drain(50 * time.Millisecond) {
		t.Fatal("Drain must report a delivery that is still in flight")
	}
	close(release)
	if !scheduler.Drain(5 * time.Second) {
		t.Fatal("Drain must return once the delivery finished")
	}
	if env.fake.count() != 1 {
		t.Fatalf("expected one delivery, got %d", env.fake.count())
	}
}
