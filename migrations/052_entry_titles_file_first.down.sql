UPDATE discussions SET title = regexp_replace(title, '^([^ —]+/[^ —]+) — (.*)$', '\2 — \1')
WHERE subject_type = 'po_entry' AND title ~ '^[^ —]+/[^ —]+ — ';
