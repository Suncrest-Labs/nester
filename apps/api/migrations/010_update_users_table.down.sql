ALTER TABLE users ADD COLUMN email TEXT;
-- We can't easily restore the NOT NULL UNIQUE without a default or updating rows, but for a down migration on local/test DB it's fine
ALTER TABLE users ALTER COLUMN email SET DEFAULT 'unknown@example.com';
UPDATE users SET email = 'unknown_' || id || '@example.com';
ALTER TABLE users ALTER COLUMN email DROP DEFAULT;
ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email);
ALTER TABLE users ALTER COLUMN email SET NOT NULL;

ALTER TABLE users ALTER COLUMN wallet_address DROP NOT NULL;
ALTER TABLE users RENAME COLUMN display_name TO name;
ALTER TABLE users DROP COLUMN kyc_status;
