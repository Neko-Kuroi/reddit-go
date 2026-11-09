CREATE TABLE comments (
    id uuid DEFAULT uuid_generate_v4() NOT NULL,
    body text NOT NULL,
    score integer DEFAULT 0 NOT NULL,
    author_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    post_id uuid NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    parent_id uuid REFERENCES comments (id) ON DELETE CASCADE,
    inserted_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (id)
);
CREATE INDEX ix_comments_post_id ON comments (post_id);
CREATE INDEX ix_comments_parent_id ON comments (parent_id);
CREATE INDEX ix_comments_author_id ON comments (author_id);
