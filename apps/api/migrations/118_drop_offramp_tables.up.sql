-- The fiat offramp was removed from the product (settlement records and saved
-- payout bank accounts). No code path reads or writes these tables any more;
-- dropping them removes the last at-rest copies of encrypted bank account
-- numbers and payout history.
DROP TABLE IF EXISTS bank_accounts;
DROP TABLE IF EXISTS settlements;
