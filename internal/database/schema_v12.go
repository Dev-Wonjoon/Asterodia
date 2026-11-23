package database

// File pins are shared across every album referencing the same file.
const galleryPinDDL = `
CREATE TABLE gallery_pin (
 media_id integer UNIQUE REFERENCES media(id) ON DELETE CASCADE,
 file_id integer UNIQUE REFERENCES media_file(id) ON DELETE CASCADE,
 created_at integer NOT NULL,
 CHECK((media_id IS NOT NULL AND file_id IS NULL) OR (media_id IS NULL AND file_id IS NOT NULL))
);
`
