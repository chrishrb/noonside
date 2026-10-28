// Package webhooks posts a system's events to the URLs its admins set up
// (table webhooks): alert.opened and alert.resolved from the alert engine,
// daily.summary and surplus.forecast from the scheduler. Events are queued in
// webhook_deliveries and sent by Sender with the notification backoff, so a
// receiver that is down gets them later. Home Assistant, an EV charger or
// any automation can act on them; the server itself never switches devices.
package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/modules/advice"
	"github.com/chrishrb/noonside/internal/modules/alerting"
	"github.com/chrishrb/noonside/internal/modules/notifications"
	"github.com/chrishrb/noonside/internal/modules/reports"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/google/uuid"
)

// Headers of every request besides the signature (notify.SignatureHeader).
const (
	EventHeader    = "X-Noonside-Event"
	DeliveryHeader = "X-Noonside-Delivery"
)

// Poster sends signed JSON (implemented by notify.Client).
type Poster interface {
	PostJSON(ctx context.Context, url, secret string, header http.Header, body []byte) error
}

// Opener decrypts sealed secrets (implemented by secretbox.Box).
type Opener interface {
	Open(sealed string) (string, error)
}

// Payload is the JSON body of a webhook request.
type Payload struct {
	ID         uuid.UUID      `json:"id"`
	Event      string         `json:"event"`
	CreatedAt  time.Time      `json:"created_at"`
	SystemID   uuid.UUID      `json:"system_id"`
	SystemName string         `json:"system_name"`
	URL        string         `json:"url"`
	Data       map[string]any `json:"data"`
}

// queued is what a delivery row stores; the envelope is completed when it
// is sent.
type queued struct {
	Path string         `json:"path"`
	Data map[string]any `json:"data"`
}

func (q queued) payload() (map[string]any, error) { return toMap(q) }

func toMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	return m, json.Unmarshal(b, &m)
}

// Service queues events and builds the scheduled ones.
type Service struct {
	store     Store
	reports   *reports.Service
	advice    *advice.Service
	publicURL string
	now       func() time.Time
	logger    *slog.Logger
}

// NewService returns the service; rep and adv build the daily summary and
// the surplus forecast.
func NewService(st Store, rep *reports.Service, adv *advice.Service, publicURL string) *Service {
	return &Service{
		store: st, reports: rep, advice: adv, publicURL: strings.TrimRight(publicURL, "/"),
		now: time.Now, logger: slog.Default().With("component", "webhooks"),
	}
}

// SetClock replaces the clock (tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// queue adds a delivery of event to each webhook in hooks that wants it;
// data renders the event for a webhook (its locale).
func (s *Service) queue(ctx context.Context, hooks []Webhook, event, dedupe, path string, data func(w Webhook) (map[string]any, error)) error {
	for _, w := range hooks {
		if !w.Wants(event) {
			continue
		}
		d, err := data(w)
		if err != nil {
			return err
		}
		p, err := queued{Path: path, Data: d}.payload()
		if err != nil {
			return err
		}
		if _, err := s.store.QueueWebhookDelivery(ctx, &WebhookDelivery{
			WebhookID: w.ID, Event: event, DedupeKey: dedupe, Payload: p,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Alert queues alert.opened or alert.resolved (typ is the notification
// type); it is the alert engine's hook. Reminders are not sent.
func (s *Service) Alert(ctx context.Context, sys systems.System, a alerting.Alert, typ, subject string) error {
	event := map[string]string{
		notifications.NotificationAlertOpened: EventAlertOpened, notifications.NotificationAlertResolved: EventAlertResolved,
	}[typ]
	if event == "" {
		return nil
	}
	hooks, err := s.store.ListWebhooks(ctx, sys.ID)
	if err != nil || len(hooks) == 0 {
		return err
	}
	path := "/s/" + sys.ID.String() + "/status"
	return s.queue(ctx, hooks, event, a.ID.String(), path, func(w Webhook) (map[string]any, error) {
		m := notifications.RenderNotification(w.Locale, notifications.Notification{
			Type: typ, Severity: a.Severity, SystemID: &sys.ID, AlertID: &a.ID,
			Payload: map[string]any{
				"kind": a.Kind, "system_name": sys.Name, "subject": subject, "details": a.Details, "timezone": sys.Timezone,
			},
		})
		d := map[string]any{
			"alert_id": a.ID, "kind": a.Kind, "severity": a.Severity, "subject": subject,
			"title": m.Title, "body": m.Body, "opened_at": a.OpenedAt, "details": a.Details,
		}
		if a.ResolvedAt != nil {
			d["resolved_at"] = *a.ResolvedAt
		} else if typ == notifications.NotificationAlertResolved {
			d["resolved_at"] = s.now()
		}
		return d, nil
	})
}

// Scheduled events go out once a day within these local hours (the
// scheduler runs every minute; a missed window is skipped, not sent late).
const (
	summaryFrom  = 6
	summaryTo    = 24
	forecastFrom = 7
	forecastTo   = 10
)

// Run queues the due daily summaries and surplus forecasts.
func (s *Service) Run(ctx context.Context, deadline time.Time) error {
	for _, ev := range []string{EventDailySummary, EventSurplusForecast} {
		hooks, err := s.store.ListWebhooksForEvent(ctx, ev)
		if err != nil {
			return err
		}
		bySystem := map[uuid.UUID][]Webhook{}
		var order []uuid.UUID
		for _, w := range hooks {
			if _, ok := bySystem[w.SystemID]; !ok {
				order = append(order, w.SystemID)
			}
			bySystem[w.SystemID] = append(bySystem[w.SystemID], w)
		}
		for _, id := range order {
			if s.now().After(deadline) {
				return nil
			}
			if err := s.runSystem(ctx, id, ev, bySystem[id]); err != nil {
				s.logger.Warn("scheduled webhook", "system_id", id, "event", ev, "error", err)
			}
		}
	}
	return nil
}

func (s *Service) runSystem(ctx context.Context, systemID uuid.UUID, event string, hooks []Webhook) error {
	sys, err := s.store.GetSystem(ctx, systemID)
	if err != nil {
		return err
	}
	local := s.now().In(sys.Loc())
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, sys.Loc())
	from, to := summaryFrom, summaryTo
	if event == EventSurplusForecast {
		from, to = forecastFrom, forecastTo
	}
	if local.Hour() < from || local.Hour() >= to {
		return nil
	}
	// Once a day: the lease records the run and blocks the rest of the window.
	unlock, ok, err := s.store.TryLockJob(ctx, "webhook:"+event+":"+sys.ID.String(), 5*time.Minute, time.Duration(to-from+1)*time.Hour)
	if err != nil || !ok {
		return err
	}
	defer unlock()
	if event == EventDailySummary {
		data, err := s.DailySummary(ctx, sys, today.AddDate(0, 0, -1))
		if err != nil {
			return err
		}
		path := fmt.Sprintf("/s/%s/overview?period=day&date=%s", sys.ID, today.AddDate(0, 0, -1).Format(time.DateOnly))
		return s.queue(ctx, hooks, event, today.AddDate(0, 0, -1).Format(time.DateOnly), path,
			func(Webhook) (map[string]any, error) { return data, nil })
	}
	a, err := s.advice.Today(ctx, sys, advice.DefaultHours)
	if err != nil {
		return err
	}
	if !a.Available || (a.Solar == nil && a.Cheap == nil) {
		return nil
	}
	data, err := toMap(AdviceJSON(a))
	if err != nil {
		return err
	}
	return s.queue(ctx, hooks, event, today.Format(time.DateOnly), "/s/"+sys.ID.String()+"/overview",
		func(Webhook) (map[string]any, error) { return data, nil })
}

// DailySummary is the data of daily.summary for the local day.
func (s *Service) DailySummary(ctx context.Context, sys *systems.System, day time.Time) (map[string]any, error) {
	rep, err := s.reports.Build(ctx, sys, reports.KindDaily, day, day.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}
	d := map[string]any{
		"date": rep.From, "energy_kwh": rep.EnergyKWh, "revenue": rep.Revenue, "currency": rep.Currency,
		"co2_avoided_kg": rep.CO2Kg,
	}
	if rep.KWhPerKWp != nil {
		d["kwh_per_kwp"] = *rep.KWhPerKWp
	}
	if rep.ExpectedKWh != nil {
		d["expected_kwh"] = *rep.ExpectedKWh
	}
	if rep.NegativePriceKWh != nil {
		d["negative_price_kwh"] = *rep.NegativePriceKWh
	}
	// Every inverter, also a single one (the report lists them only for
	// several).
	invs, err := s.store.ListInverters(ctx, sys.ID)
	if err != nil {
		return nil, err
	}
	bs, err := s.store.Energy(ctx, sys.ID, "day", day, day.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}
	per := map[uuid.UUID]float64{}
	for _, b := range bs {
		per[b.InverterID] += b.EnergyWh / 1000
	}
	lines := []map[string]any{}
	for _, inv := range invs {
		lines = append(lines, map[string]any{"name": inv.Name, "energy_kwh": round(per[inv.ID], 2)})
	}
	d["inverters"] = lines
	// Peak of the whole system from the 5-minute curve.
	pts, err := s.store.PowerCurve(ctx, sys.ID, day, day.AddDate(0, 0, 1), 5*time.Minute)
	if err != nil {
		return nil, err
	}
	total := map[time.Time]float64{}
	for _, p := range pts {
		total[p.Time] += p.PacW
	}
	var peak float64
	var peakAt time.Time
	for t, w := range total {
		if w > peak || (w == peak && t.Before(peakAt)) {
			peak, peakAt = w, t
		}
	}
	if peak > 0 {
		d["peak_w"], d["peak_at"] = round(peak, 0), peakAt.UTC()
	}
	return d, nil
}

// --- sending ---------------------------------------------------------------

const (
	sendBatch = 50
	// claimLease hides claimed deliveries from other runs until they are
	// completed; a killed run's deliveries are retried after it.
	claimLease = 5 * time.Minute
	// keepDeliveries is how long sent and failed deliveries are listed.
	keepDeliveries = 30 * 24 * time.Hour
)

// Sender posts queued deliveries.
type Sender struct {
	store     Store
	poster    Poster
	box       Opener
	publicURL string
	now       func() time.Time
	logger    *slog.Logger
}

// NewSender returns the sender; box may be nil if no webhook has a secret.
func NewSender(st Store, poster Poster, box Opener, publicURL string) *Sender {
	return &Sender{
		store: st, poster: poster, box: box, publicURL: strings.TrimRight(publicURL, "/"),
		now: time.Now, logger: slog.Default().With("component", "webhooks"),
	}
}

// Run sends due deliveries until none are left or deadline passes, and
// prunes old ones about once an hour.
func (s *Sender) Run(ctx context.Context, deadline time.Time) error {
	unlock, ok, err := s.store.TryLockJob(ctx, "webhooks_dispatch", claimLease, 0)
	if err != nil || !ok {
		return err
	}
	defer unlock()
	names := map[uuid.UUID]string{}
	for s.now().Before(deadline) {
		batch, err := s.store.ClaimWebhookDeliveries(ctx, sendBatch, claimLease)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			break
		}
		for _, d := range batch {
			s.complete(ctx, d, s.send(ctx, d, names))
		}
	}
	if unlockPrune, ok, err := s.store.TryLockJob(ctx, "webhooks_prune", time.Minute, time.Hour); err == nil && ok {
		defer unlockPrune()
		if _, err := s.store.PruneWebhookDeliveries(ctx, s.now().Add(-keepDeliveries)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sender) send(ctx context.Context, d WebhookDelivery, names map[uuid.UUID]string) error {
	w := d.Webhook
	name, ok := names[w.SystemID]
	if !ok {
		sys, err := s.store.GetSystem(ctx, w.SystemID)
		if err != nil {
			return err
		}
		name, names[w.SystemID] = sys.Name, sys.Name
	}
	var q queued
	b, err := json.Marshal(d.Payload)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &q); err != nil {
		return err
	}
	return Send(ctx, s.poster, s.box, *w, Payload{
		ID: d.EventID, Event: d.Event, CreatedAt: d.CreatedAt.UTC(), SystemID: w.SystemID, SystemName: name,
		URL: s.publicURL + q.Path, Data: q.Data,
	})
}

func (s *Sender) complete(ctx context.Context, d WebhookDelivery, sendErr error) {
	ctx = context.WithoutCancel(ctx)
	var err error
	switch {
	case sendErr == nil:
		err = s.store.CompleteWebhookDelivery(ctx, d.ID, "sent", "", nil)
	case d.Attempts >= notifications.MaxAttempts:
		s.logger.Warn("webhook delivery failed", "webhook_id", d.WebhookID, "event", d.Event, "error", sendErr)
		err = s.store.CompleteWebhookDelivery(ctx, d.ID, "failed", sendErr.Error(), nil)
	default:
		retry := notifications.RetryAt(d.Attempts, s.now())
		err = s.store.CompleteWebhookDelivery(ctx, d.ID, "pending", sendErr.Error(), &retry)
	}
	if err != nil {
		s.logger.Error("record webhook delivery", "delivery_id", d.ID, "error", err)
	}
}

// Send posts p to the webhook now, signed with its secret.
func Send(ctx context.Context, poster Poster, box Opener, w Webhook, p Payload) error {
	secret := ""
	if w.SecretEnc != "" {
		if box == nil {
			return errors.New("server has no secret key configured")
		}
		var err error
		if secret, err = box.Open(w.SecretEnc); err != nil {
			return err
		}
	}
	if p.Data == nil {
		p.Data = map[string]any{}
	}
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	h := http.Header{}
	h.Set(EventHeader, p.Event)
	h.Set(DeliveryHeader, p.ID.String())
	return poster.PostJSON(ctx, w.URL, secret, h, body)
}

// TestPayload is the body of the test button's request.
func TestPayload(publicURL string, sys *systems.System, now time.Time) Payload {
	return Payload{
		ID: uuid.New(), Event: EventTest, CreatedAt: now.UTC(), SystemID: sys.ID, SystemName: sys.Name,
		URL:  strings.TrimRight(publicURL, "/") + "/s/" + sys.ID.String() + "/settings/webhooks",
		Data: map[string]any{"message": "Test from noonside"},
	}
}

// --- JSON of the advice ------------------------------------------------------

// AdviceWindow, AdviceSlot and AdviceData mirror the Advice schema of the
// API for the surplus.forecast payload.
type AdviceWindow struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	ExpectedKWh float64   `json:"expected_kwh"`
	MeanW       float64   `json:"mean_w"`
	MeanEURMWh  *float64  `json:"mean_eur_mwh,omitempty"`
}

type AdviceSlot struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	ExpectedW *float64  `json:"expected_w,omitempty"`
	EURMWh    *float64  `json:"eur_mwh,omitempty"`
}

type AdvicePrice struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	EURMWh float64   `json:"eur_mwh"`
}

type AdviceData struct {
	Available      bool          `json:"available"`
	Date           string        `json:"date"`
	Hours          int           `json:"hours"`
	ExpectedKWh    *float64      `json:"expected_kwh,omitempty"`
	RemainingKWh   *float64      `json:"remaining_kwh,omitempty"`
	SolarWindow    *AdviceWindow `json:"solar_window,omitempty"`
	CheapWindow    *AdviceWindow `json:"cheap_window,omitempty"`
	NegativePrices []AdvicePrice `json:"negative_prices"`
	Slots          []AdviceSlot  `json:"slots"`
}

// AdviceJSON converts advice for a payload.
func AdviceJSON(a *advice.Advice) AdviceData {
	win := func(w *advice.Window) *AdviceWindow {
		if w == nil {
			return nil
		}
		return &AdviceWindow{Start: w.Start.UTC(), End: w.End.UTC(), ExpectedKWh: w.ExpectedKWh, MeanW: w.MeanW, MeanEURMWh: w.MeanEURMWh}
	}
	out := AdviceData{
		Available: a.Available, Date: a.Date.Format(time.DateOnly), Hours: a.Hours,
		ExpectedKWh: a.ExpectedKWh, RemainingKWh: a.RemainingKWh,
		SolarWindow: win(a.Solar), CheapWindow: win(a.Cheap),
		NegativePrices: []AdvicePrice{}, Slots: []AdviceSlot{},
	}
	for _, p := range a.Negative {
		out.NegativePrices = append(out.NegativePrices, AdvicePrice{Start: p.Start.UTC(), End: p.End.UTC(), EURMWh: p.EURMWh})
	}
	for _, s := range a.Slots {
		out.Slots = append(out.Slots, AdviceSlot{Start: s.Start.UTC(), End: s.End.UTC(), ExpectedW: s.ExpectedW, EURMWh: s.EURMWh})
	}
	return out
}

func round(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}
