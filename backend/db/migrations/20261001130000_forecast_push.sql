-- Opt-in morning push with the day's expected yield from the forecast.
ALTER TABLE notification_prefs ADD COLUMN forecast_push boolean NOT NULL DEFAULT false;
