-- Who saved a wiki page and when (shown as the page's edit history)
CREATE TABLE wiki_edits (
    id         BIGSERIAL PRIMARY KEY,
    page_id    BIGINT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    user_id    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_wiki_edits_page ON wiki_edits (page_id, created_at DESC);

-- existing pages: their creation and, when different, their last save
INSERT INTO wiki_edits (page_id, user_id, created_at)
SELECT id, created_by, created_at FROM wiki_pages;
INSERT INTO wiki_edits (page_id, user_id, created_at)
SELECT id, updated_by, updated_at FROM wiki_pages WHERE updated_at > created_at;
