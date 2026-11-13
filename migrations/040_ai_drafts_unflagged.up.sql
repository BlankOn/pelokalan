-- AI drafts no person has saved yet (origin = 'ai') get their own status ("AI perlu ditinjau") instead of the
-- fuzzy flag, so they can be told apart from fuzzy entries. Downloads still mark them fuzzy.
UPDATE entries SET fuzzy = false WHERE origin = 'ai' AND fuzzy;
