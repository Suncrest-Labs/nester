-- Adds the identity fields the KYC submission handler already reads from
-- the request but has never persisted (nester#1190): full_name,
-- date_of_birth, country. A user who submitted a complete KYC record had
-- no way to tell these were silently dropped — the record looked accepted
-- from the outside while missing the fields a compliance review needs.
--
-- Plaintext for now, matching kyc_documents' own original shape before the
-- later id_number/object-key encryption pass (migration 069) — full_name
-- and date_of_birth are sensitive PII and a genuine candidate for the same
-- encrypted-column treatment, but that is a separate, deliberate follow-up
-- rather than silently bundled into the fix for the underlying data-loss
-- bug. country is a coarse jurisdiction identifier, not sensitive on its
-- own.
ALTER TABLE kyc_documents
    ADD COLUMN full_name     TEXT,
    ADD COLUMN date_of_birth DATE,
    ADD COLUMN country       TEXT;
