package alerting

import (
	"math"
	"time"
)

// SunElevation returns the sun's elevation above the horizon in degrees at
// t for the given position (low-precision solar position, good to ~1°).
func SunElevation(t time.Time, lat, lon float64) float64 {
	const rad = math.Pi / 180
	d := float64(t.UTC().UnixNano())/float64(24*time.Hour) + 2440587.5 - 2451545.0 // days since J2000
	g := (357.529 + 0.98560028*d) * rad
	q := 280.459 + 0.98564736*d
	l := (q + 1.915*math.Sin(g) + 0.020*math.Sin(2*g)) * rad
	e := (23.439 - 0.00000036*d) * rad
	ra := math.Atan2(math.Cos(e)*math.Sin(l), math.Cos(l)) / rad
	dec := math.Asin(math.Sin(e) * math.Sin(l))
	gmst := math.Mod(18.697374558+24.06570982441908*d, 24)
	ha := (gmst*15 + lon - ra) * rad
	la := lat * rad
	return math.Asin(math.Sin(la)*math.Sin(dec)+math.Cos(la)*math.Cos(dec)*math.Cos(ha)) / rad
}
