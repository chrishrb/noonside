package email

import "sort"

// samples holds one example of every email, for previews and tests.
var samples = map[string]func() Email{}

// Register makes an email previewable (`noonside email preview <name>`) and
// includes it in the render tests. Call it from init() next to the email type.
func Register(sample func() Email) {
	samples[sample().Name()] = sample
}

// Sample returns the registered example for name.
func Sample(name string) (Email, bool) {
	f, ok := samples[name]
	if !ok {
		return nil, false
	}
	return f(), true
}

// Registered lists the names of all registered emails.
func Registered() []string {
	out := make([]string, 0, len(samples))
	for n := range samples {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
