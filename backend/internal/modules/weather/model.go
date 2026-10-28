package weather

import (
	"time"
)

// Forecast is the normalized Open-Meteo forecast (stored as JSON in the cache).
type Forecast struct {
	Current Current `json:"current"`
	// Slots are 15-minute values; each is the mean over the slot before End.
	Slots []Slot `json:"slots"`
	Hours []Hour `json:"hours"`
	// Days are local calendar days, starting today.
	Days []Day `json:"days"`
}

type Current struct {
	Time          time.Time `json:"time"`
	TemperatureC  float64   `json:"temperature_c"`
	WeatherCode   int       `json:"weather_code"`
	CloudCoverPct float64   `json:"cloud_cover_pct"`
	IsDay         bool      `json:"is_day"`
}

type Slot struct {
	End time.Time `json:"end"`
	// Minutes is the slot length; 0 means 15.
	Minutes int `json:"minutes,omitempty"`
	// GTI is the global irradiance on the panel plane in W/m².
	GTI          float64 `json:"gti"`
	TemperatureC float64 `json:"temperature_c"`
}

type Hour struct {
	Time          time.Time `json:"time"`
	WeatherCode   int       `json:"weather_code"`
	TemperatureC  float64   `json:"temperature_c"`
	CloudCoverPct float64   `json:"cloud_cover_pct"`
	PrecipProbPct float64   `json:"precip_prob_pct"`
}

type Day struct {
	Date        string    `json:"date"`
	WeatherCode int       `json:"weather_code"`
	TempMinC    float64   `json:"temp_min_c"`
	TempMaxC    float64   `json:"temp_max_c"`
	Sunrise     time.Time `json:"sunrise"`
	Sunset      time.Time `json:"sunset"`
	SunshineH   float64   `json:"sunshine_h"`
}

// Len is the slot's length.
func (s Slot) Len() time.Duration {
	if s.Minutes == 0 {
		return SlotLen
	}
	return time.Duration(s.Minutes) * time.Minute
}

// Start is the beginning of the slot.
func (s Slot) Start() time.Time { return s.End.Add(-s.Len()) }

// Mid is the middle of the slot, where its mean value is plotted.
func (s Slot) Mid() time.Time { return s.End.Add(-s.Len() / 2) }

const (
	// SlotLen is the length of a forecast slot.
	SlotLen = 15 * time.Minute
	// performanceRatio covers inverter, wiring, soiling and mismatch losses.
	performanceRatio = 0.85
	// tempCoeff is the power loss per kelvin of cell temperature above 25 °C
	// (typical crystalline silicon).
	tempCoeff = -0.004
)

// ExpectedPower is the expected AC power in W of kwp peak power at
// irradiance gti (W/m² on the panel plane) and air temperature tempC. The
// cell temperature is estimated with the NOCT approximation.
func ExpectedPower(gti, tempC, kwp float64) float64 {
	if gti <= 0 || kwp <= 0 {
		return 0
	}
	cell := tempC + gti/800*20
	p := gti / 1000 * kwp * 1000 * performanceRatio * (1 + tempCoeff*(cell-25))
	return max(p, 0)
}

// ExpectedAt interpolates the expected power in W at t between the slot
// midpoints; ok is false outside the forecast.
func (f *Forecast) ExpectedAt(t time.Time, kwp float64) (w float64, ok bool) {
	return ExpectedAt(f.Slots, t, kwp)
}

// ExpectedAt interpolates the expected power in W at t between the slot
// midpoints; ok is false outside the slots.
func ExpectedAt(slots []Slot, t time.Time, kwp float64) (w float64, ok bool) {
	for i := range slots {
		m := slots[i].Mid()
		if t.Before(m) {
			if i == 0 {
				// First half of the first slot.
				return slots[0].Power(kwp), !t.Before(slots[0].Start())
			}
			prev := slots[i-1].Mid()
			frac := float64(t.Sub(prev)) / float64(m.Sub(prev))
			a, b := slots[i-1].Power(kwp), slots[i].Power(kwp)
			return a + (b-a)*frac, true
		}
	}
	if n := len(slots); n > 0 && t.Before(slots[n-1].End) {
		return slots[n-1].Power(kwp), true
	}
	return 0, false
}

// Power is the expected power in W of kwp during the slot.
func (s Slot) Power(kwp float64) float64 { return ExpectedPower(s.GTI, s.TemperatureC, kwp) }

// ExpectedKWh sums the expected energy of the slots within [from, to).
func (f *Forecast) ExpectedKWh(from, to time.Time, kwp float64) float64 {
	return ExpectedKWh(f.Slots, from, to, kwp)
}

// ExpectedKWh sums the expected energy of the slots within [from, to).
func ExpectedKWh(slots []Slot, from, to time.Time, kwp float64) float64 {
	var wh float64
	for _, s := range slots {
		if !s.Start().Before(from) && !s.End.After(to) {
			wh += s.Power(kwp) * s.Len().Hours()
		}
	}
	return wh / 1000
}
