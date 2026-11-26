-- Entry discussions are titled "owner/file — source text" (was "source text — owner/file"): the file comes first.
UPDATE discussions SET title = regexp_replace(title, '^(.*) — ([^ —]+/[^ —]+)$', '\2 — \1')
WHERE subject_type = 'po_entry' AND title ~ ' — [^ —]+/[^ —]+$';
