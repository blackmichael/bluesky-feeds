CREATE TABLE cursors (
	service TEXT PRIMARY KEY,
	cursor_value BIGINT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);