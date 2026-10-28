package systemsapi

import (
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
)

// toSystem converts a system for the API.
func toSystem(s *systems.System, role auth.Role) *generated.System {
	return &generated.System{
		ID:                     s.ID,
		Name:                   s.Name,
		ImageURL:               httpapi.OptMediaURL(s.ImageKey),
		Location:               s.Location,
		Latitude:               httpapi.OptFloat(s.Latitude),
		Longitude:              httpapi.OptFloat(s.Longitude),
		Timezone:               s.Timezone,
		PeakPowerKwp:           s.PeakPowerKWp,
		CommissionedAt:         httpapi.OptDate(s.CommissionedAt),
		Orientation:            s.Orientation,
		Planes:                 toPlanes(s.Planes),
		ModuleInfo:             s.ModuleInfo,
		Operator:               s.Operator,
		TariffPerKwh:           s.TariffPerKWh,
		Currency:               s.Currency,
		Co2KgPerKwh:            s.CO2KgPerKWh,
		Role:                   generated.Role(role),
		MinuteRetentionYears:   httpapi.OptNilInt(s.MinuteRetentionYears),
		PriceZone:              toSystemZone(s),
		UnpaidAtNegativePrices: s.UnpaidAtNegativePrices,
		CreatedAt:              s.CreatedAt,
		UpdatedAt:              s.UpdatedAt,
	}
}

func toSystemZone(s *systems.System) generated.OptNilSystemPriceZone {
	if s.PriceZone == nil {
		return generated.OptNilSystemPriceZone{}
	}
	return generated.NewOptNilSystemPriceZone(generated.SystemPriceZone(*s.PriceZone))
}

func toPlanes(ps []systems.Plane) []generated.Plane {
	out := make([]generated.Plane, len(ps))
	for i, p := range ps {
		out[i] = generated.Plane{ID: generated.NewOptUUID(p.ID), Kwp: p.KWp, TiltDeg: p.TiltDeg, AzimuthDeg: p.AzimuthDeg}
		if p.Name != "" {
			out[i].Name = generated.NewOptString(p.Name)
		}
	}
	return out
}

func toInverter(i *systems.Inverter) generated.Inverter {
	return generated.Inverter{
		ID:           i.ID,
		ExternalRef:  i.ExternalRef,
		Name:         i.Name,
		Model:        i.Model,
		SerialNumber: i.SerialNumber,
		PeakPowerKwp: i.PeakPowerKWp,
		MaxAcPowerW:  httpapi.OptFloat(i.MaxACPowerW),
		PlaneID:      httpapi.OptUUID(i.PlaneID),
		Color:        i.Color,
		SortOrder:    i.SortOrder,
	}
}

func toMember(m *systems.Member) *generated.Member {
	return &generated.Member{
		UserID: m.UserID, Email: m.Email, Name: m.Name, Role: generated.Role(m.Role), CreatedAt: m.CreatedAt,
		AvatarURL: httpapi.OptMediaURL(m.AvatarKey),
	}
}

func toInvitation(i *systems.Invitation) generated.Invitation {
	return generated.Invitation{
		ID: i.ID, Email: i.Email, Role: generated.Role(i.Role), CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt,
	}
}

func toAPIKey(k *systems.APIKey) generated.APIKey {
	return generated.APIKey{
		ID: k.ID, Name: k.Name, Prefix: k.Prefix, Scopes: keyScopes(k.Scopes), CreatedAt: k.CreatedAt, LastUsedAt: httpapi.OptTime(k.LastUsedAt),
	}
}

func keyScopes(ss []string) []generated.APIKeyScope {
	out := make([]generated.APIKeyScope, len(ss))
	for i, s := range ss {
		out[i] = generated.APIKeyScope(s)
	}
	return out
}

func scopeStrings(s []auth.Scope) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = string(v)
	}
	return out
}
