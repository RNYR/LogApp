package jobs

// Rental alerts: one Discord message per save that started or changed a rental.
//
// The API calls AnnounceRentals after the save has committed. Delivery runs in
// the background so a slow or unreachable Discord never delays or fails a
// rental; every attempt is still recorded in notification_deliveries (trigger
// "rental") and audited like the overdue report. Rental deliveries are kept out
// of the overdue report's per-item/per-day dedupe (see notifiedToday).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"sklad/server/internal/notify"
	"sklad/server/internal/settings"
)

// DefaultRentalTimeout bounds one rental delivery including retries.
const DefaultRentalTimeout = 2 * time.Minute

// RentalChange is one item whose rental a save started or changed.
type RentalChange struct {
	AssetTag       string
	Name           string
	SN             string
	Type           string
	PreviousStatus string
	Status         string
	StatusColor    string
	Renter         string
	SentDate       string
	ReturnDate     string
	Note           string
}

// AnnounceRentals posts the rental changes of one save in the background. It
// returns immediately; Drain waits for deliveries still in flight.
func (s *Scheduler) AnnounceRentals(changes []RentalChange, actor string) {
	if len(changes) == 0 {
		return
	}
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		ctx, cancel := context.WithTimeout(context.Background(), s.rentalTimeout)
		defer cancel()
		if err := s.NotifyRentals(ctx, changes, actor); err != nil {
			s.log.Warn("rental notification failed", "items", len(changes), "error", err)
		}
	}()
}

// Drain waits up to timeout for background rental deliveries to finish. Call
// it after the HTTP server has stopped and before the database is closed; it
// reports whether everything finished in time.
func (s *Scheduler) Drain(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() { s.background.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// NotifyRentals posts one message for the rental changes of one save. It does
// nothing (and returns nil) unless Discord and rental alerts are both enabled
// and a webhook is configured.
func (s *Scheduler) NotifyRentals(ctx context.Context, changes []RentalChange, actor string) error {
	if len(changes) == 0 {
		return nil
	}
	current, err := settings.Load(ctx, s.db)
	if err != nil {
		return fmt.Errorf("jobs: load settings: %w", err)
	}
	if !current.DiscordEnabled || !current.RentalAlerts || !current.WebhookConfigured() {
		return nil
	}
	webhook, err := s.webhookURL(current)
	if err != nil {
		return err
	}

	total := len(changes)
	if len(changes) > s.maxItems {
		changes = changes[:s.maxItems]
	}
	items := make([]notify.Item, 0, len(changes))
	tags := make([]string, 0, len(changes))
	for _, change := range changes {
		items = append(items, notify.Item{
			AssetTag:       change.AssetTag,
			Name:           change.Name,
			SN:             change.SN,
			Type:           change.Type,
			Status:         change.Status,
			StatusColor:    change.StatusColor,
			PreviousStatus: change.PreviousStatus,
			Renter:         change.Renter,
			SentDate:       change.SentDate,
			ReturnDate:     change.ReturnDate,
			Note:           change.Note,
		})
		tags = append(tags, change.AssetTag)
	}

	now := s.now()
	report := notify.Report{
		Kind:         notify.KindRental,
		Items:        items,
		TotalItems:   total,
		GeneratedAt:  now,
		Organization: current.OrganizationName,
		AppURL:       s.appURL,
		Locale:       current.DefaultLocale,
		Actor:        actor,
	}

	payload := deliveryPayload{AssetTags: tags, DueCount: total, Truncated: total > len(changes)}
	key, err := rentalKey(now)
	if err != nil {
		return err
	}
	deliveryID, alreadySent, err := s.reserveDelivery(ctx, webhook, notify.KindRental, key, current, payload)
	if err != nil {
		return err
	}
	if alreadySent {
		return nil
	}

	notifier, err := s.notifier(webhook)
	if err != nil {
		s.completeDelivery(ctx, deliveryID, statusFailed, payload, err)
		return fmt.Errorf("%w: %w", ErrDeliveryFailed, err)
	}
	result, sendErr := notifier.Send(ctx, report)
	payload.Messages = result.Messages
	payload.MessageIDs = result.MessageIDs

	run := RunResult{Trigger: notify.KindRental, NewCount: len(items), DeliveryID: &deliveryID,
		Messages: result.Messages, Embeds: result.Embeds, Sent: sendErr == nil}
	if sendErr != nil {
		s.completeDelivery(ctx, deliveryID, statusFailed, payload, sendErr)
		s.auditRun(ctx, notify.KindRental, &run, sendErr)
		return fmt.Errorf("%w: %w", ErrDeliveryFailed, sendErr)
	}
	s.completeDelivery(ctx, deliveryID, statusSent, payload, nil)
	s.auditRun(ctx, notify.KindRental, &run, nil)
	return nil
}

// rentalKey makes each save's delivery unique: rental alerts are events, not a
// daily digest, so two saves of the same item are two messages — even within
// the same clock tick.
func rentalKey(now time.Time) (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("jobs: rental delivery key: %w", err)
	}
	return fmt.Sprintf("rental:%d:%s", now.UnixNano(), hex.EncodeToString(random)), nil
}
