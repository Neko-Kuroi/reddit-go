DROP INDEX ix_users_username;
DROP INDEX ix_users_email;
CREATE UNIQUE INDEX ix_users_username_lower ON users (lower(username));
CREATE UNIQUE INDEX ix_users_email_lower ON users (lower(email));
DROP INDEX ix_subreddits_name;
CREATE UNIQUE INDEX ix_subreddits_name_lower ON subreddits (lower(name));
