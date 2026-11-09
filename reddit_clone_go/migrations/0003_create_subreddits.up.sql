CREATE TABLE subreddits (
    id uuid DEFAULT uuid_generate_v4() NOT NULL,
    name character varying(21) NOT NULL,
    description text,
    owner_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    inserted_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX ix_subreddits_name ON subreddits (name);
CREATE INDEX ix_subreddits_owner_id ON subreddits (owner_id);
