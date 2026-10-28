-- Profile and system pictures (issue #45). The keys name immutable objects
-- in the media store (internal/platform/media), served under /media/<key>.
ALTER TABLE profiles ADD COLUMN avatar_key text;
ALTER TABLE systems ADD COLUMN image_key text;
