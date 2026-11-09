CREATE TABLE users (
    id uuid DEFAULT uuid_generate_v4() NOT NULL,
    username character varying(20) NOT NULL,
    email character varying(255) NOT NULL,
    password_hash character varying(255) NOT NULL,
    post_karma integer DEFAULT 0 NOT NULL,
    comment_karma integer DEFAULT 0 NOT NULL,
    inserted_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX ix_users_username ON users (username);
CREATE UNIQUE INDEX ix_users_email ON users (email);
