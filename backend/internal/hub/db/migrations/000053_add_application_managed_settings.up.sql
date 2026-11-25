-- The settings of an application that come from x-orcacd in its compose file.
ALTER TABLE applications ADD COLUMN managed_settings TEXT NOT NULL DEFAULT '[]';
