-- Drop the submission scaffold introduced by 099_chain_submissions (#1153).
--
-- chain_submissions and account_sequences existed only to serve
-- SubmissionPipeline, which never had a non-test caller: it was never
-- constructed in main.go, so AllocateSequence, RecordSubmission,
-- ResolveTimeout and RecoverOnStartup never ran outside its own tests. Both
-- tables have therefore always been empty, so dropping them removes no data
-- and changes no behaviour.
--
-- Durable submission tracking is not being lost here -- it was built
-- separately and correctly for #1085. SubmissionStore writes
-- submission_intents, is wired in main.go, and SubmissionReconciler sweeps it
-- against the chain. That path derives a transaction's real chain identity
-- through IdentifyTransaction; the scaffold's computeTransactionHash instead
-- SHA-256'd the base64 envelope string, a digest of our own making that the
-- chain has never heard of. Its other two internals were stubs in the same
-- spirit: checkOnChainStatus returned StatusUnknown unconditionally and
-- seedSequenceFromNetwork returned 0.
--
-- Keeping the tables would earn nothing and cost clarity: empty tables named
-- chain_submissions and account_sequences read as "submission recovery and
-- sequence reservation happen here" to anyone auditing the money path, when
-- in fact neither ever ran.
--
-- Per-account sequence reservation (#1089) is not implemented by this schema
-- either -- account_sequences was only ever read by the same dead code path.
-- Reinstating a reservation scheme is a design decision to make against the
-- live invoker, not a matter of refilling this table.

DROP TABLE IF EXISTS chain_submissions;

DROP TABLE IF EXISTS account_sequences;
