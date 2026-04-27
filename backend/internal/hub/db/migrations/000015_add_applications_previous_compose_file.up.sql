ALTER TABLE applications
    ADD COLUMN previous_compose_file TEXT NOT NULL DEFAULT '';
