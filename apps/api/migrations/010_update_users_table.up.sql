ALTER TABLE users ADD COLUMN kyc_status TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE users RENAME COLUMN name TO display_name;

-- We conditionally drop constraints to allow structural alterations if needed, but since email is NOT NULL, dropping the column is sufficient.
ALTER TABLE users DROP COLUMN email;

-- Make wallet_address NOT NULL
ALTER TABLE users ALTER COLUMN wallet_address SET NOT NULL;
