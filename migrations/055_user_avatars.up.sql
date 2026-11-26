-- profile pictures: the blob key (blobs.key) of the user's 256×256 JPEG, '' = none
ALTER TABLE users ADD COLUMN avatar TEXT NOT NULL DEFAULT '';
