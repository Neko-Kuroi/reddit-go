DROP INDEX ix_subreddits_name_lower;
CREATE UNIQUE INDEX ix_subreddits_name ON subreddits (name);
DROP INDEX ix_users_email_lower;
DROP INDEX ix_users_username_lower;
CREATE UNIQUE INDEX ix_users_email ON users (email);
CREATE UNIQUE INDEX ix_users_username ON users (username);
