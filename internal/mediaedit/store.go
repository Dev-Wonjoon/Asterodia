package mediaedit

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type Store struct{ db *sql.DB }

func raw(value any) string { b, _ := json.Marshal(value); return string(b) }
func now() int64           { return time.Now().UnixMilli() }
func (s Store) session(ctx context.Context, id string) (Session, error) {
	v := Session{}
	var options string
	err := s.db.QueryRowContext(ctx, `SELECT id,source_file_id,source_title,request_json,state,width,height,frame_count,progress,error_message,cancel_requested,created_at,render_version FROM media_edit_session WHERE id=?`, id).Scan(&v.ID, &v.FileID, &v.Title, &options, &v.State, &v.Width, &v.Height, &v.FrameCount, &v.Progress, &v.Error, &v.Cancelling, &v.CreatedAt, &v.RenderVersion)
	if err == nil {
		err = json.Unmarshal([]byte(options), &v.Options)
	}
	v.Cancelling = v.Cancelling && (v.State == "pending" || v.State == "preparing")
	return v, err
}
func (s Store) job(ctx context.Context, id string) (Job, error) {
	v := Job{}
	var options, usage string
	err := s.db.QueryRowContext(ctx, `SELECT id,session_id,request_json,state,progress,result_file_id,error_message,cancel_requested,created_at,preview,resources_json FROM media_edit_job WHERE id=?`, id).Scan(&v.ID, &v.SessionID, &options, &v.State, &v.Progress, &v.ResultFileID, &v.Error, &v.Cancelling, &v.CreatedAt, &v.Preview, &usage)
	if err == nil {
		err = json.Unmarshal([]byte(options), &v.Options)
	}
	if err == nil {
		err = json.Unmarshal([]byte(usage), &v.Resources)
	}
	v.Cancelling = v.Cancelling && (v.State == "pending" || v.State == "running" || v.State == "verifying")
	if v.State == "completed" && v.Error != "" {
		v.OutputNotice, v.Error = v.Error, ""
	}
	return v, err
}
func (s Store) ids(ctx context.Context, table, where string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM "+table+" "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s Store) state(table, id, state, message string) error {
	_, err := s.db.ExecContext(context.Background(), "UPDATE "+table+" SET state=?,error_message=?,updated_at=? WHERE id=?", state, message, now(), id)
	return err
}
