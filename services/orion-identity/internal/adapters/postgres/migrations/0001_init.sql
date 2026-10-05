CREATE TABLE IF NOT EXISTS identity_users (
	id         TEXT PRIMARY KEY,
	username   TEXT UNIQUE NOT NULL,
	password   TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS identity_projects (
	id         TEXT PRIMARY KEY,
	name       TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS identity_user_project_roles (
	user_id    TEXT NOT NULL REFERENCES identity_users(id) ON DELETE CASCADE,
	project_id TEXT NOT NULL REFERENCES identity_projects(id) ON DELETE CASCADE,
	role       TEXT NOT NULL,
	PRIMARY KEY (user_id, project_id, role)
);

CREATE TABLE IF NOT EXISTS identity_user_system_roles (
	user_id TEXT NOT NULL REFERENCES identity_users(id) ON DELETE CASCADE,
	role    TEXT NOT NULL,
	PRIMARY KEY (user_id, role)
);

CREATE TABLE IF NOT EXISTS identity_tokens (
	value      TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL,
	payload    JSONB NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	issued_at  TIMESTAMPTZ NOT NULL
);
