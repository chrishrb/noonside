package notifications

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLocalesHaveTheSamePhrases(t *testing.T) {
	en := phrases["en"]
	for _, l := range Locales {
		assert.Len(t, phrases[l], len(en), l)
		for k, v := range en {
			assert.NotEmpty(t, phrases[l][k], "%s %s", l, k)
			assert.NotEmpty(t, v)
		}
	}
	assert.Equal(t, "es", NormLocale("es-ES"))
	assert.Equal(t, "de", NormLocale("de-AT"))
	assert.Equal(t, "en", NormLocale("fr"))
}
