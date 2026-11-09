CREATE TABLE subreddit_members (
    id uuid DEFAULT uuid_generate_v4() NOT NULL,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    subreddit_id uuid NOT NULL REFERENCES subreddits (id) ON DELETE CASCADE,
    role character varying(20) DEFAULT 'member' NOT NULL,
    inserted_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX ix_subreddit_members_unique ON subreddit_members (user_id, subreddit_id);
CREATE INDEX ix_subreddit_members_user_id ON subreddit_members (user_id);
CREATE INDEX ix_subreddit_members_subreddit_id ON subreddit_members (subreddit_id);
