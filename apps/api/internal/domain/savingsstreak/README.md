# Savings gamification rules

The gamification engine awards progress only for confirmed net saving actions.
A deposit qualifies when:

- the event type is `deposit_confirmed`
- both gross and net amounts are at least `5`
- the deposit was not withdrawn within the 24 hour churn window
- the event has not already been processed

Streak days are calculated in the user's timezone. One qualifying event can
advance only one local day. A single one-day gap can be covered by grace once;
larger gaps reset the streak.

Durable score is calculated from total net saved, longest streak, and completed
goals. Achievements are awarded once per user and are deduped separately from
the event log.
