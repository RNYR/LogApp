package jobs

import (
	"context"
	"log/slog"
	"time"
)

type BackupStore interface {
	BackupDB(context.Context) (string, error)
}

// StartBackups makes a consistent SQLite backup at startup and after each interval.
// Failed attempts retry after one minute. Cancellation interrupts an active backup.
func StartBackups(ctx context.Context, st BackupStore, log *slog.Logger, interval time.Duration) {
	runBackups(ctx, st, log, interval, time.Minute)
}
func runBackups(ctx context.Context, st BackupStore, log *slog.Logger, interval, retry time.Duration) {
	for ctx.Err() == nil {
		attempt, cancel := context.WithTimeout(ctx, 5*time.Minute)
		path, err := st.BackupDB(attempt)
		cancel()
		if ctx.Err() != nil {
			return
		}
		delay := interval
		if err != nil {
			log.Error("database backup failed; retrying", "error", err, "retry_after", retry)
			delay = retry
		} else {
			log.Info("database backup completed", "path", path, "next_backup_in", interval)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
