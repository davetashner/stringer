-- Database schema

CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    -- TODO: Add email validation constraint
    email TEXT
);

-- FIXME: Missing index on created_at for time-range queries
CREATE TABLE events (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    created_at TIMESTAMP DEFAULT NOW()
);
