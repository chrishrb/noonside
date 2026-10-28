package advice

import (
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/prices"
	"github.com/chrishrb/noonside/internal/modules/weather"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var berlin, _ = time.LoadLocation("Europe/Berlin")

// bell returns 15-minute slots of a local day with irradiance rising from
// 06:00 to a peak at peak and falling to 20:00.
func bell(day time.Time, peak time.Time) []weather.Slot {
	var out []weather.Slot
	for t := day; t.Before(day.AddDate(0, 0, 1)); t = t.Add(15 * time.Minute) {
		end := t.Add(15 * time.Minute)
		var gti float64
		if t.Hour() >= 6 && t.Hour() < 20 {
			d := t.Sub(peak).Hours()
			gti = max(0, 900-120*d*d)
		}
		out = append(out, weather.Slot{End: end, GTI: gti, TemperatureC: 20})
	}
	return out
}

func hourPrices(day time.Time, values map[int]float64, def float64) []prices.MarketPrice {
	var out []prices.MarketPrice
	for h := 0; h < 24; h++ {
		s := day.Add(time.Duration(h) * time.Hour)
		v, ok := values[h]
		if !ok {
			v = def
		}
		out = append(out, prices.MarketPrice{Zone: "DE-LU", Start: s, End: s.Add(time.Hour), EURMWh: v})
	}
	return out
}

func TestCompute(t *testing.T) {
	day := time.Date(2026, 6, 15, 0, 0, 0, 0, berlin)
	slots := bell(day, day.Add(13*time.Hour))
	ps := hourPrices(day, map[int]float64{12: -10, 13: -20, 19: 20, 20: 10, 21: 15}, 80)

	a := Compute(day.Add(8*time.Hour+7*time.Minute), berlin, slots, 10, ps, 3)
	require.True(t, a.Available)
	assert.Equal(t, day, a.Date)
	assert.Equal(t, 3, a.Hours)
	require.NotNil(t, a.ExpectedKWh)
	require.NotNil(t, a.RemainingKWh)
	assert.GreaterOrEqual(t, *a.ExpectedKWh, *a.RemainingKWh, "nothing before 08:00")

	// The bell is symmetric around 13:00: 11:30–14:30.
	require.NotNil(t, a.Solar)
	assert.Equal(t, day.Add(11*time.Hour+30*time.Minute), a.Solar.Start.In(berlin))
	assert.Equal(t, day.Add(14*time.Hour+30*time.Minute), a.Solar.End.In(berlin))
	assert.Greater(t, a.Solar.ExpectedKWh, 10.0)
	assert.InDelta(t, a.Solar.ExpectedKWh*1000/3, a.Solar.MeanW, 50)
	require.NotNil(t, a.Solar.MeanEURMWh)

	// Cheapest three hours: 12–15 averages (-10 -20 80)/3 = 16.7, which
	// beats 19–22 at 15.
	require.NotNil(t, a.Cheap)
	assert.Equal(t, day.Add(19*time.Hour), a.Cheap.Start.In(berlin))
	assert.Equal(t, 15.0, *a.Cheap.MeanEURMWh)

	require.Len(t, a.Negative, 1)
	assert.Equal(t, day.Add(12*time.Hour), a.Negative[0].Start.In(berlin))
	assert.Equal(t, day.Add(14*time.Hour), a.Negative[0].End.In(berlin))

	// Hourly slots from the current hour to midnight.
	require.Len(t, a.Slots, 16)
	assert.Equal(t, day.Add(8*time.Hour), a.Slots[0].Start.In(berlin))
	assert.Equal(t, 80.0, *a.Slots[0].EURMWh)
	assert.Equal(t, 0.0, *a.Slots[len(a.Slots)-1].ExpectedW)
}

func TestComputeLateAndPartial(t *testing.T) {
	day := time.Date(2026, 6, 15, 0, 0, 0, 0, berlin)
	slots := bell(day, day.Add(13*time.Hour))

	// After sunset nothing is left to shift loads to.
	a := Compute(day.Add(21*time.Hour), berlin, slots, 10, nil, 3)
	assert.True(t, a.Available)
	assert.Nil(t, a.Solar)
	assert.Nil(t, a.Cheap, "no prices")
	assert.Empty(t, a.Negative)
	assert.Equal(t, 0.0, *a.RemainingKWh)

	// Less than the window left: the rest of the day.
	a = Compute(day.Add(22*time.Hour+30*time.Minute), berlin, nil, 0, hourPrices(day, nil, 42), 3)
	assert.True(t, a.Available)
	assert.Nil(t, a.ExpectedKWh)
	require.NotNil(t, a.Cheap)
	assert.Equal(t, day.Add(22*time.Hour+30*time.Minute), a.Cheap.Start.In(berlin))
	assert.Equal(t, day.AddDate(0, 0, 1), a.Cheap.End.In(berlin))
	assert.Nil(t, a.Slots[0].ExpectedW)

	// Neither forecast nor prices.
	a = Compute(day.Add(10*time.Hour), berlin, nil, 10, nil, 0)
	assert.False(t, a.Available)
	assert.Equal(t, DefaultHours, a.Hours)
	assert.Nil(t, a.Solar)
}
