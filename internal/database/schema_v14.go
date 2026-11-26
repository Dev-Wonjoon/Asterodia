package database

// Automatic profile albums reference post files without changing their identity.
const instagramProfileAlbumDDL = `
CREATE TABLE instagram_profile_album (
 owner_id text PRIMARY KEY NOT NULL CHECK(length(owner_id)>0),
 media_id integer NOT NULL UNIQUE REFERENCES user_album(media_id) ON DELETE CASCADE
) WITHOUT ROWID;
CREATE TABLE instagram_profile_source (
 source_media_id integer PRIMARY KEY REFERENCES media(id) ON DELETE CASCADE,
 album_id integer NOT NULL REFERENCES instagram_profile_album(media_id) ON DELETE CASCADE
);
CREATE INDEX idx_instagram_profile_source ON instagram_profile_source(album_id,source_media_id);
`
