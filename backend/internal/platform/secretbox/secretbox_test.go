package secretbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoundTrip(t *testing.T) {
	key, err := GenerateKey()
	require.NoError(t, err)
	b, err := New(key)
	require.NoError(t, err)

	sealed, err := b.Seal("hunter2")
	require.NoError(t, err)
	assert.NotContains(t, sealed, "hunter2")

	pt, err := b.Open(sealed)
	require.NoError(t, err)
	assert.Equal(t, "hunter2", pt)

	other, _ := GenerateKey()
	b2, _ := New(other)
	_, err = b2.Open(sealed)
	assert.Error(t, err)
}
