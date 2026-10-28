package httpapi

import (
	"time"

	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/chrishrb/noonside/internal/platform/media"
	"github.com/google/uuid"
)

// OptMediaURL is the URL of a stored picture, unset without one.
func OptMediaURL(key *string) generated.OptString {
	if u := media.URL(key); u != "" {
		return generated.NewOptString(u)
	}
	return generated.OptString{}
}

func OptNilInt(v *int) generated.OptNilInt {
	if v == nil {
		return generated.OptNilInt{}
	}
	return generated.NewOptNilInt(*v)
}

func OptFloat(v *float64) generated.OptFloat64 {
	if v == nil {
		return generated.OptFloat64{}
	}
	return generated.NewOptFloat64(*v)
}

func OptInt(v *int) generated.OptInt {
	if v == nil {
		return generated.OptInt{}
	}
	return generated.NewOptInt(*v)
}

func OptTime(v *time.Time) generated.OptDateTime {
	if v == nil {
		return generated.OptDateTime{}
	}
	return generated.NewOptDateTime(*v)
}

func OptDate(v *time.Time) generated.OptDate {
	if v == nil {
		return generated.OptDate{}
	}
	return generated.NewOptDate(*v)
}

func OptUUID(v *uuid.UUID) generated.OptUUID {
	if v == nil {
		return generated.OptUUID{}
	}
	return generated.NewOptUUID(*v)
}

// PtrOf returns the value of an optional, or nil.
func PtrOf[T any](o interface{ Get() (T, bool) }) *T {
	if v, ok := o.Get(); ok {
		return &v
	}
	return nil
}
