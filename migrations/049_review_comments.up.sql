-- Review comments: a comment by an owner/manager on an entry of a file that is waiting for review (module_reviews)
-- belongs to that review and stays open (a red dot on the entry) until the translator, its author or a manager
-- resolves it. Approval waits until no review comment of the file is open.
ALTER TABLE comments
    ADD COLUMN review_id BIGINT REFERENCES module_reviews(id) ON DELETE SET NULL,
    ADD COLUMN resolved_at TIMESTAMPTZ,
    ADD COLUMN resolved_by BIGINT REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX comments_open_review ON comments (review_id) WHERE review_id IS NOT NULL AND resolved_at IS NULL AND deleted_at IS NULL;
