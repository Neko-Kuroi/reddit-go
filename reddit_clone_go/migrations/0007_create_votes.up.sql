CREATE TABLE votes (
    id uuid DEFAULT uuid_generate_v4() NOT NULL,
    value integer NOT NULL,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    target_type character varying(10) NOT NULL,
    target_id uuid NOT NULL,
    inserted_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX ix_votes_unique ON votes (user_id, target_type, target_id);
CREATE INDEX ix_votes_target ON votes (target_type, target_id);
