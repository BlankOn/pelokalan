-- The link a PO file was downloaded from ("Unggah berkas" by URL), so it can be downloaded again
-- later ("Perbarui berkas…" in the file menu). Empty for files uploaded from disk.
ALTER TABLE catalogs ADD COLUMN source_url TEXT NOT NULL DEFAULT '';
