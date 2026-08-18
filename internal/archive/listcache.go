package archive

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// PutChannels writes a slice of LIST results to list_cache. A LIST on a large
// network is tens of thousands of channels, which is exactly why they go
// straight to disk in batches instead of living in RAM (blueprint §5.4): call
// this as the replies stream in, once per chunk.
//
// A channel already cached is updated, so a re-LIST refreshes rather than
// duplicates.
func (d *DB) PutChannels(ctx context.Context, chans []model.ChannelInfo, fetchedAt time.Time) error {
	if len(chans) == 0 {
		return nil
	}
	tx, err := d.w.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("archive: list cache: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful commit

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO list_cache(channel, users, topic, fetched_at) VALUES(?, ?, ?, ?)
ON CONFLICT(channel) DO UPDATE SET
  users = excluded.users, topic = excluded.topic, fetched_at = excluded.fetched_at`)
	if err != nil {
		return fmt.Errorf("archive: list cache: %w", err)
	}
	defer stmt.Close()

	ms := fetchedAt.UnixMilli()
	if fetchedAt.IsZero() {
		ms = 0
	}
	for _, c := range chans {
		if c.Name == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, c.Name, c.Users, c.Topic, ms); err != nil {
			return fmt.Errorf("archive: list cache %q: %w", c.Name, err)
		}
	}
	return tx.Commit()
}

// Channels reads the cached channel list, biggest first, optionally narrowed
// to names or topics containing match (case-insensitively). limit 0 means the
// package default page size.
func (d *DB) Channels(ctx context.Context, match string, limit int) ([]model.ChannelInfo, error) {
	where := ""
	var args []any
	if m := strings.TrimSpace(match); m != "" {
		where = ` WHERE channel LIKE ? ESCAPE '\' OR topic LIKE ? ESCAPE '\'`
		pat := "%" + escapeLike(m) + "%"
		args = append(args, pat, pat)
	}
	args = append(args, pageLimit(limit))

	rows, err := d.r.QueryContext(ctx,
		`SELECT channel, users, topic FROM list_cache`+where+` ORDER BY users DESC, channel LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("archive: read list cache: %w", err)
	}
	defer rows.Close()
	var out []model.ChannelInfo
	for rows.Next() {
		var (
			c     model.ChannelInfo
			users sql.NullInt64
			topic sql.NullString
		)
		if err := rows.Scan(&c.Name, &users, &topic); err != nil {
			return nil, fmt.Errorf("archive: read list cache: %w", err)
		}
		c.Users = int(users.Int64)
		c.Topic = topic.String
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListFetchedAt is when the cache was last refreshed, so the UI can say "as of
// an hour ago" instead of pretending the numbers are live. A zero time means
// the cache is empty.
func (d *DB) ListFetchedAt(ctx context.Context) (time.Time, error) {
	var ms sql.NullInt64
	if err := d.r.QueryRowContext(ctx, `SELECT MAX(fetched_at) FROM list_cache`).Scan(&ms); err != nil {
		return time.Time{}, fmt.Errorf("archive: list cache age: %w", err)
	}
	if !ms.Valid || ms.Int64 == 0 {
		return time.Time{}, nil
	}
	return time.UnixMilli(ms.Int64).UTC(), nil
}

// ClearChannels empties the LIST cache.
func (d *DB) ClearChannels(ctx context.Context) error {
	if _, err := d.w.ExecContext(ctx, `DELETE FROM list_cache`); err != nil {
		return fmt.Errorf("archive: clear list cache: %w", err)
	}
	return nil
}

// escapeLike neutralises the wildcards in a user's search string, so typing a
// % in the Discover box searches for a percent sign.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
