CREATE TABLE posts (
    id uuid DEFAULT uuid_generate_v4() NOT NULL,
    title character varying(300) NOT NULL,
    type character varying(10) NOT NULL,
    body text,
    url character varying(2048),
    score integer DEFAULT 0 NOT NULL,
    hot_score double precision DEFAULT '0'::double precision NOT NULL,
    author_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    subreddit_id uuid NOT NULL REFERENCES subreddits (id) ON DELETE CASCADE,
    inserted_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (id)
);
CREATE INDEX ix_posts_author_id ON posts (author_id);
CREATE INDEX ix_posts_subreddit_id ON posts (subreddit_id);
CREATE INDEX ix_posts_subreddit_hot ON posts (subreddit_id, hot_score);
CREATE INDEX ix_posts_subreddit_new ON posts (subreddit_id, inserted_at);
CREATE INDEX ix_posts_hot_score ON posts (hot_score);
-- 注: hot_score の既定値は Python 版と同じ 0.0。PG 上の既定値の表記まで
-- Python 版（SQLAlchemy が文字列として渡す形）に一致させるため、この書き方にしている。
