-- Rollback safety: key_version is the ONLY mapping from a ciphertext to the key
-- that sealed it. Dropping it once any row has been rotated to a non-v1 key
-- would leave those ciphertexts undecryptable. Abort rather than lose data.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM kyc_documents WHERE key_version <> 'v1') THEN
        RAISE EXCEPTION
            'cannot drop key_version: % row(s) are encrypted with a non-v1 key; re-key them to v1 before rolling back',
            (SELECT COUNT(*) FROM kyc_documents WHERE key_version <> 'v1');
    END IF;
END $$;

ALTER TABLE kyc_documents DROP COLUMN IF EXISTS key_version;
ALTER TABLE kyc_documents DROP COLUMN IF EXISTS back_object_key_encrypted;
ALTER TABLE kyc_documents DROP COLUMN IF EXISTS front_object_key_encrypted;
ALTER TABLE kyc_documents DROP COLUMN IF EXISTS id_number_fingerprint;
ALTER TABLE kyc_documents DROP COLUMN IF EXISTS id_number_encrypted;
