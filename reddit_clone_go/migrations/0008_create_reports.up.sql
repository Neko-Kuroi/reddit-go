CREATE TABLE reports (
    id uuid DEFAULT uuid_generate_v4() NOT NULL,
    reason character varying(50) NOT NULL,
    reporter_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    target_type character varying(10) NOT NULL,
    target_id uuid NOT NULL,
    inserted_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (id)
);
CREATE INDEX ix_reports_target ON reports (target_type, target_id);
CREATE INDEX ix_reports_reporter_id ON reports (reporter_id);
