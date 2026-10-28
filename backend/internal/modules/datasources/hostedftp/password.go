package hostedftp

import (
	"crypto/rand"
	"encoding/base32"
	"math/big"
	"strings"
)

// Password charset: letters and digits without look-alikes, because the
// password is typed into the device by hand. The Solar-Log has problems with
// special characters and long passwords; the one hyphen in the middle keeps
// the format the accounts on the former Storage Box had.
const (
	lower  = "abcdefghijkmnopqrstuvwxyz"
	upper  = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	digits = "23456789"
)

// GeneratePassword returns a random password of 11 letters and digits (at
// least one lower case, one upper case and one digit) with a hyphen in the
// middle, 12 characters in all; the Solar-Log rejects longer passwords.
// About 64 bits of randomness.
func GeneratePassword() (string, error) {
	const n = 11
	all := lower + upper + digits
	pw := make([]byte, 0, n)
	for _, set := range []string{lower, upper, digits} {
		ch, err := pick(set)
		if err != nil {
			return "", err
		}
		pw = append(pw, ch)
	}
	for len(pw) < n {
		ch, err := pick(all)
		if err != nil {
			return "", err
		}
		pw = append(pw, ch)
	}
	// Shuffle, so the classes are not at fixed positions.
	for i := len(pw) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		pw[i], pw[j.Int64()] = pw[j.Int64()], pw[i]
	}
	return string(pw[:n/2]) + "-" + string(pw[n/2:]), nil // 5 + "-" + 6
}

func pick(set string) (byte, error) {
	i, err := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
	if err != nil {
		return 0, err
	}
	return set[i.Int64()], nil
}

// GenerateUsername returns a short random username ("pv" and 8 lower case
// base32 characters, 40 bits), easy to type into the device.
func GenerateUsername() (string, error) {
	var b [5]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "pv" + strings.ToLower(base32.StdEncoding.EncodeToString(b[:])), nil
}
