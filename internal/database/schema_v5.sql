CREATE TABLE media_directory (
    id integer PRIMARY KEY AUTOINCREMENT NOT NULL,
    directory_path text NOT NULL UNIQUE CHECK(length(directory_path)>0),
    directory_key text NOT NULL CHECK(length(directory_key)>0),
    key_version integer NOT NULL CHECK(key_version IN (0,1)),
    registered integer NOT NULL DEFAULT 0 CHECK(registered IN (0,1)),
    import_media_id integer REFERENCES local_folder(media_id) ON DELETE SET NULL,
    created_at integer NOT NULL,
    CHECK(key_version<>0 OR directory_key='recorded-v1:'||directory_path),
    CHECK(key_version<>1 OR substr(directory_key,1,11)='winpath-v1:'),
    UNIQUE(directory_key,key_version)
);
CREATE INDEX idx_directory_registered ON media_directory(registered,id);

CREATE TABLE file_move_job (
    id text PRIMARY KEY NOT NULL,
    target_directory_id integer NOT NULL REFERENCES media_directory(id) ON DELETE RESTRICT,
    target_directory_snapshot text NOT NULL CHECK(length(target_directory_snapshot)>0),
    state text NOT NULL CHECK(state IN ('pending','running','pausing','paused','completed','completed_with_errors','cancelled','interrupted','failed')),
    pause_requested_at integer,
    counts_json text NOT NULL DEFAULT '{}' CHECK(json_valid(counts_json)),
    revision integer NOT NULL DEFAULT 0 CHECK(revision>=0),
    created_at integer NOT NULL,
    updated_at integer NOT NULL
);
CREATE INDEX idx_move_job_state ON file_move_job(state,created_at);
CREATE TABLE file_move_item (
    id text PRIMARY KEY NOT NULL,
    job_id text NOT NULL REFERENCES file_move_job(id) ON DELETE CASCADE,
    media_id integer,
    file_id integer,
    media_id_snapshot integer NOT NULL,
    file_uuid_snapshot text NOT NULL,
    source_path text NOT NULL CHECK(length(source_path)>0),
    source_key text NOT NULL CHECK(length(source_key)>0),
    target_path text,
    target_key text,
    expected_stat_json text NOT NULL CHECK(json_valid(expected_stat_json)),
    verification_json text CHECK(verification_json IS NULL OR json_valid(verification_json)),
    state text NOT NULL CHECK(state IN ('pending','copying','verified','committed','cleanup_pending','completed','failed','excluded')),
    commit_token text NOT NULL UNIQUE,
    committed_at integer,
    finished_at integer,
    try_count integer NOT NULL DEFAULT 0 CHECK(try_count>=0),
    error_code text,
    error_message text,
    FOREIGN KEY(media_id,file_id) REFERENCES media_file(media_id,id) ON DELETE SET NULL,
    UNIQUE(job_id,file_uuid_snapshot),
    CHECK((media_id IS NULL)=(file_id IS NULL)),
    CHECK((target_path IS NULL)=(target_key IS NULL)),
    CHECK(target_path IS NULL OR (length(target_path)>0 AND length(target_key)>0)),
    CHECK(committed_at IS NULL OR (target_path IS NOT NULL AND verification_json IS NOT NULL AND state IN ('committed','cleanup_pending','completed'))),
    CHECK(state NOT IN ('committed','cleanup_pending','completed') OR committed_at IS NOT NULL)
);
CREATE INDEX idx_move_item_job_state ON file_move_item(job_id,state,id);
CREATE UNIQUE INDEX uq_move_active_file ON file_move_item(file_id) WHERE file_id IS NOT NULL AND state IN ('pending','copying','verified','committed','cleanup_pending');
CREATE UNIQUE INDEX uq_move_active_target ON file_move_item(target_key) WHERE target_key IS NOT NULL AND state IN ('pending','copying','verified','committed','cleanup_pending');

CREATE TRIGGER v5_file_uuid_immutable
BEFORE UPDATE OF file_uuid ON media_file
WHEN NEW.file_uuid<>OLD.file_uuid
BEGIN SELECT RAISE(ABORT,'FILE_UUID_IMMUTABLE'); END;
CREATE TRIGGER v5_directory_path_guard
BEFORE UPDATE OF directory_path ON media_directory
WHEN NEW.directory_path<>OLD.directory_path AND (
  EXISTS(SELECT 1 FROM media_file WHERE directory_id=OLD.id) OR
  EXISTS(SELECT 1 FROM local_folder_location WHERE directory_id=OLD.id) OR
  EXISTS(SELECT 1 FROM file_move_job WHERE target_directory_id=OLD.id)
)
BEGIN SELECT RAISE(ABORT,'DIRECTORY_IN_USE'); END;
CREATE TRIGGER v5_move_file_insert
BEFORE INSERT ON file_move_item
WHEN NEW.file_id IS NULL OR NOT EXISTS(
  SELECT 1 FROM media_file WHERE id=NEW.file_id AND media_id=NEW.media_id
    AND file_uuid=NEW.file_uuid_snapshot AND media_id=NEW.media_id_snapshot
)
BEGIN SELECT RAISE(ABORT,'MOVE_FILE_MISMATCH'); END;
CREATE TRIGGER v5_move_item_identity
BEFORE UPDATE OF job_id,media_id_snapshot,file_uuid_snapshot,source_path,source_key,commit_token ON file_move_item
WHEN NEW.job_id<>OLD.job_id OR NEW.media_id_snapshot<>OLD.media_id_snapshot OR
 NEW.file_uuid_snapshot<>OLD.file_uuid_snapshot OR NEW.source_path<>OLD.source_path OR
 NEW.source_key<>OLD.source_key OR NEW.commit_token<>OLD.commit_token
BEGIN SELECT RAISE(ABORT,'MOVE_IDENTITY_IMMUTABLE'); END;
CREATE TRIGGER v5_move_file_update
BEFORE UPDATE OF media_id,file_id ON file_move_item
WHEN (NEW.file_id IS NOT NULL AND (NEW.file_id IS NOT OLD.file_id OR NEW.media_id IS NOT OLD.media_id))
 OR (NEW.file_id IS NULL AND OLD.state IN ('pending','copying','verified','committed','cleanup_pending'))
BEGIN SELECT RAISE(ABORT,'MOVE_FILE_MISMATCH'); END;
CREATE TRIGGER v5_move_commit_immutable
BEFORE UPDATE OF target_path,target_key,verification_json,committed_at ON file_move_item
WHEN OLD.committed_at IS NOT NULL AND (
 NEW.target_path IS NOT OLD.target_path OR NEW.target_key IS NOT OLD.target_key OR
 NEW.verification_json IS NOT OLD.verification_json OR NEW.committed_at IS NOT OLD.committed_at)
BEGIN SELECT RAISE(ABORT,'MOVE_COMMIT_IMMUTABLE'); END;
CREATE TRIGGER v5_move_destination_immutable
BEFORE UPDATE OF target_directory_id,target_directory_snapshot ON file_move_job
WHEN NEW.target_directory_id IS NOT OLD.target_directory_id OR NEW.target_directory_snapshot IS NOT OLD.target_directory_snapshot
BEGIN SELECT RAISE(ABORT,'MOVE_DESTINATION_IMMUTABLE'); END;
CREATE TRIGGER v5_move_file_delete
BEFORE DELETE ON media_file
WHEN EXISTS(SELECT 1 FROM file_move_item WHERE file_id=OLD.id AND state IN ('pending','copying','verified','committed','cleanup_pending'))
BEGIN SELECT RAISE(ABORT,'JOB_ACTIVE'); END;
CREATE TRIGGER v5_move_job_delete
BEFORE DELETE ON file_move_job
WHEN EXISTS(SELECT 1 FROM file_move_item WHERE job_id=OLD.id AND state IN ('pending','copying','verified','committed','cleanup_pending'))
BEGIN SELECT RAISE(ABORT,'JOB_ACTIVE'); END;
CREATE TRIGGER v5_checkpoint_file_insert
BEFORE INSERT ON local_import_target
WHEN NEW.media_file_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM media_file WHERE id=NEW.media_file_id AND file_uuid=NEW.file_uuid_snapshot)
BEGIN SELECT RAISE(ABORT,'CHECKPOINT_FILE_MISMATCH'); END;
CREATE TRIGGER v5_checkpoint_file_update
BEFORE UPDATE OF media_file_id,file_uuid_snapshot ON local_import_target
WHEN NEW.media_file_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM media_file WHERE id=NEW.media_file_id AND file_uuid=NEW.file_uuid_snapshot)
BEGIN SELECT RAISE(ABORT,'CHECKPOINT_FILE_MISMATCH'); END;
