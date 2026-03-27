ALTER TABLE users
ADD COLUMN IF NOT EXISTS wallet_address TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS users_wallet_address_unique_idx
ON users (wallet_address)
WHERE wallet_address IS NOT NULL;
