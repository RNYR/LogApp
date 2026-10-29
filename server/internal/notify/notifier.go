// Package notify delivers Sklad notifications to an external chat service.
//
// The package is deliberately dependency-free (standard library only, no
// knowledge of the database or the HTTP API): the overdue scheduler and the
// admin "send test message" endpoint both build a Report and hand it to a
// Notifier. Today Discord is the only implementation; the Notifier interface is
// what a second channel would implement.
package notify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Kind identifies what triggered a notification. The values match
// notification_deliveries.trigger in docs/contracts/schema.md.
type Kind string

// Trigger kinds.
const (
	KindOverdueDaily Kind = "overdue_daily"
	KindManual       Kind = "manual"
	KindTest         Kind = "test"
)

// ChannelDiscord is the name reported by the Discord implementation.
const ChannelDiscord = "discord"

// Item is one inventory row as it appears in a notification.
//
// It is a rendering type, not a database row: the caller decides which items are
// worth reporting and fills in what the message needs.
type Item struct {
	AssetTag    string
	Name        string
	SN          string
	Type        string
	Status      string
	StatusColor string // "#rrggbb" badge colour, used as the embed accent
	SentDate    string // YYYY-MM-DD
	ReturnDate  string // YYYY-MM-DD
	Note        string

	// DaysOverdue is how many days past ReturnDate the item is (0 = due today).
	DaysOverdue int
}

// Report is one delivery request.
type Report struct {
	Kind Kind

	// Title and Summary override the generated wording when set.
	Title   string
	Summary string

	Items []Item

	// TotalItems is how many items the report was drawn from when Items had to
	// be capped; 0 (or a value <= len(Items)) means the list is complete.
	TotalItems int

	// GeneratedAt defaults to the time of the Send call.
	GeneratedAt time.Time

	// Organization is shown in the footer; AppURL becomes a link back to Sklad.
	Organization string
	AppURL       string

	// Locale selects the message language (en, cs, it); anything else falls back
	// to English.
	Locale string
}

// Result summarises a delivery.
type Result struct {
	// Messages is the number of chat messages posted.
	Messages int
	// Embeds is the number of embeds delivered across those messages.
	Embeds int
	// MessageIDs are the platform message ids collected via ?wait=true.
	MessageIDs []string
	// Attempts counts HTTP requests including retries, for the delivery log.
	Attempts int
}

// Notifier delivers a Report to one channel.
//
// Send must be safe to call with a cancelled context, must not leak webhook
// secrets into error strings, and should report partial delivery through the
// returned Result alongside the error.
type Notifier interface {
	// Name identifies the channel implementation (for example "discord").
	Name() string
	// Send delivers the report.
	Send(ctx context.Context, report Report) (Result, error)
}

// DeliveryError describes a failed delivery.
//
// Permanent marks failures that retrying cannot fix (Discord rejected the
// payload or the webhook no longer exists); the API turns those into 422 so the
// administrator knows to fix the configuration, while transient failures become
// 502.
type DeliveryError struct {
	Channel   string
	Status    int    // HTTP status when the failure came from the remote end
	Permanent bool   // true when retrying the same request cannot help
	Message   string // remote error text, already stripped of secrets
	Err       error  // underlying transport error, if any
}

// Error renders the failure without ever including the webhook URL or token.
func (e *DeliveryError) Error() string {
	channel := e.Channel
	if channel == "" {
		channel = ChannelDiscord
	}
	parts := []string{channel + " delivery failed"}
	if e.Status != 0 {
		parts = append(parts, fmt.Sprintf("status %d", e.Status))
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	} else if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	return strings.Join(parts, ": ")
}

// Unwrap exposes the underlying transport error.
func (e *DeliveryError) Unwrap() error {
	return e.Err
}

// IsPermanent reports whether err is a permanent delivery failure.
func IsPermanent(err error) bool {
	var delivery *DeliveryError
	if errors.As(err, &delivery) {
		return delivery.Permanent
	}
	return false
}

// Status returns the HTTP status behind err, or 0.
func Status(err error) int {
	var delivery *DeliveryError
	if errors.As(err, &delivery) {
		return delivery.Status
	}
	return 0
}

// resolveTitle returns the report title, or a sensible default for its kind.
func (r Report) resolveTitle(l labels) string {
	if title := strings.TrimSpace(r.Title); title != "" {
		return title
	}
	switch r.Kind {
	case KindTest:
		return l.TestSubject
	case KindManual:
		if len(r.Items) == 1 {
			return l.ManualSubjectOne
		}
		return fmt.Sprintf(l.ManualSubjectMany, len(r.Items))
	default:
		if len(r.Items) == 1 {
			return l.OverdueSubjectOne
		}
		return fmt.Sprintf(l.OverdueSubjectMany, len(r.Items))
	}
}

// resolveSummary returns the report's summary line.
func (r Report) resolveSummary(l labels) string {
	if summary := strings.TrimSpace(r.Summary); summary != "" {
		return summary
	}
	if len(r.Items) == 0 {
		return l.NoItems
	}

	total := len(r.Items)
	if r.TotalItems > total {
		total = r.TotalItems
	}
	lines := []string{fmt.Sprintf(l.ItemCount, total)}
	if r.TotalItems > len(r.Items) {
		lines = append(lines, fmt.Sprintf(l.Truncated, len(r.Items), r.TotalItems))
	}
	if r.Kind == KindOverdueDaily || r.Kind == KindManual {
		overdue := 0
		for _, item := range r.Items {
			if item.DaysOverdue > 0 {
				overdue++
			}
		}
		if overdue > 0 {
			lines = append(lines, fmt.Sprintf(l.OverdueCount, overdue))
		}
	}
	return strings.Join(lines, "\n")
}

// labelsFor returns the notification wording for a locale. These strings are
// server-side (the SPA catalogs under web/src/locales cover the interface); an
// unknown locale falls back to English.
func labelsFor(locale string) labels {
	switch strings.ToLower(strings.TrimSpace(locale)) {
	case "cs":
		return labels{
			OverdueSubjectOne:  "Sklad — 1 položka po termínu",
			OverdueSubjectMany: "Sklad — %d položek po termínu",
			ManualSubjectOne:   "Sklad — kontrola po termínu (1 položka)",
			ManualSubjectMany:  "Sklad — kontrola po termínu (%d položek)",
			TestSubject:        "Sklad — testovací oznámení",
			ItemCount:          "Položek: %d",
			OverdueCount:       "Po termínu: %d",
			NoItems:            "Žádné položky nevyžadují pozornost.",
			FieldAssetTag:      "Inventární číslo",
			FieldName:          "Název",
			FieldType:          "Typ",
			FieldStatus:        "Stav",
			FieldSN:            "Sériové číslo",
			FieldSent:          "Vydáno",
			FieldReturn:        "Vrátit do",
			FieldNote:          "Poznámka",
			DaysOverdue:        "Po termínu o %d dní",
			Generated:          "Vygenerováno",
			Truncated:          "Zobrazeno prvních %d z %d",
			Footer:             "Sklad — správa majetku",
			SampleName:         "Testovací kamera",
			SampleType:         "Kamera",
			SampleStatus:       "Test",
			SampleNote:         "Testovací zpráva z administrace Sklad — není třeba nic dělat.",
		}
	case "it":
		return labels{
			OverdueSubjectOne:  "Sklad — 1 articolo scaduto",
			OverdueSubjectMany: "Sklad — %d articoli scaduti",
			ManualSubjectOne:   "Sklad — controllo scadenze (1 articolo)",
			ManualSubjectMany:  "Sklad — controllo scadenze (%d articoli)",
			TestSubject:        "Sklad — notifica di prova",
			ItemCount:          "Articoli: %d",
			OverdueCount:       "Scaduti: %d",
			NoItems:            "Nessun articolo richiede attenzione.",
			FieldAssetTag:      "Codice inventario",
			FieldName:          "Nome",
			FieldType:          "Tipo",
			FieldStatus:        "Stato",
			FieldSN:            "Numero di serie",
			FieldSent:          "Inviato",
			FieldReturn:        "Da restituire",
			FieldNote:          "Nota",
			DaysOverdue:        "Scaduto da %d giorni",
			Generated:          "Generato",
			Truncated:          "Mostrati i primi %d di %d",
			Footer:             "Sklad — gestione attrezzature",
			SampleName:         "Telecamera di prova",
			SampleType:         "Telecamera",
			SampleStatus:       "Test",
			SampleNote:         "Messaggio di prova dal pannello Sklad — nessuna azione richiesta.",
		}
	default:
		return labels{
			OverdueSubjectOne:  "Sklad — 1 item overdue",
			OverdueSubjectMany: "Sklad — %d items overdue",
			ManualSubjectOne:   "Sklad — overdue check (1 item)",
			ManualSubjectMany:  "Sklad — overdue check (%d items)",
			TestSubject:        "Sklad — test notification",
			ItemCount:          "Items: %d",
			OverdueCount:       "Overdue: %d",
			NoItems:            "No items need attention.",
			FieldAssetTag:      "Asset tag",
			FieldName:          "Name",
			FieldType:          "Type",
			FieldStatus:        "Status",
			FieldSN:            "Serial number",
			FieldSent:          "Sent",
			FieldReturn:        "Return by",
			FieldNote:          "Note",
			DaysOverdue:        "Overdue by %d day(s)",
			Generated:          "Generated",
			Truncated:          "Showing the first %d of %d",
			Footer:             "Sklad — equipment inventory",
			SampleName:         "Test camera",
			SampleType:         "Camera",
			SampleStatus:       "Test",
			SampleNote:         "Test message from the Sklad admin panel — nothing to do.",
		}
	}
}

// labels holds the wording of one notification in one language.
type labels struct {
	OverdueSubjectOne  string
	OverdueSubjectMany string
	ManualSubjectOne   string
	ManualSubjectMany  string
	TestSubject        string
	ItemCount          string
	OverdueCount       string
	NoItems            string
	FieldAssetTag      string
	FieldName          string
	FieldType          string
	FieldStatus        string
	FieldSN            string
	FieldSent          string
	FieldReturn        string
	FieldNote          string
	DaysOverdue        string
	Generated          string
	Truncated          string
	Footer             string
	SampleName         string
	SampleType         string
	SampleStatus       string
	SampleNote         string
}

// SampleReport builds the report used by the admin "send test message" action:
// one synthetic item, so the message can be verified end to end without
// exposing real inventory data. The wording follows the requested locale.
func SampleReport(locale, organization, appURL string, generatedAt time.Time) Report {
	l := labelsFor(locale)
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	return Report{
		Kind:    KindTest,
		Summary: l.SampleNote,
		Items: []Item{{
			AssetTag:    "PV-TEST",
			Name:        l.SampleName,
			SN:          "SKLAD-TEST-0001",
			Type:        l.SampleType,
			Status:      l.SampleStatus,
			StatusColor: "#3498DB",
			SentDate:    generatedAt.AddDate(0, 0, -7).Format("2006-01-02"),
			ReturnDate:  generatedAt.AddDate(0, 0, 7).Format("2006-01-02"),
		}},
		GeneratedAt:  generatedAt,
		Organization: organization,
		AppURL:       appURL,
		Locale:       locale,
	}
}
