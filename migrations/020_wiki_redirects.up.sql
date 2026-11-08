-- Old wiki page addresses keep working after a page's address is changed
CREATE TABLE wiki_redirects (
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    slug       TEXT NOT NULL,
    page_id    BIGINT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    PRIMARY KEY (project_id, slug)
);
