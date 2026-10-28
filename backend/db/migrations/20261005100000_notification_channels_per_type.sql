-- Channels per notification type: alerts, yield reports and the morning
-- forecast each go to their own set of channels. "in_app" means the
-- notification counts as unread (bell and badge); it is always listed.
ALTER TABLE notification_prefs
  ADD COLUMN alert_channels text[] NOT NULL DEFAULT '{in_app,email,push,ntfy,webhook}'
    CHECK (alert_channels <@ '{in_app,email,push,ntfy,webhook}'),
  ADD COLUMN report_channels text[] NOT NULL DEFAULT '{email}'
    CHECK (report_channels <@ '{in_app,email,push,ntfy,webhook}'),
  ADD COLUMN forecast_channels text[] NOT NULL DEFAULT '{push}'
    CHECK (forecast_channels <@ '{in_app,email,push,ntfy,webhook}');

-- Reports went only by email and the forecast only by push.
UPDATE notification_prefs SET
  alert_channels = array_remove(ARRAY[
    CASE WHEN in_app THEN 'in_app' END, CASE WHEN email THEN 'email' END, CASE WHEN push THEN 'push' END,
    CASE WHEN ntfy THEN 'ntfy' END, CASE WHEN webhook THEN 'webhook' END], NULL),
  report_channels = CASE WHEN email THEN '{email}'::text[] ELSE '{}' END,
  forecast_channels = CASE WHEN push THEN '{push}'::text[] ELSE '{}' END;

-- in_app, email, push, ntfy and webhook are no longer read; a later
-- migration drops them (the previous release still reads them while this
-- one is deployed).
