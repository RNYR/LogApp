package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type backupProbe struct {
	calls chan int
	n     int
}

func (p *backupProbe) BackupDB(ctx context.Context) (string, error) {
	p.n++
	p.calls <- p.n
	if p.n == 1 {
		return "", errors.New("disk temporarily unavailable")
	}
	if p.n == 3 {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return "test.db", nil
}
func TestBackupsRetryRepeatAndCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &backupProbe{calls: make(chan int, 3)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runBackups(ctx, probe, slog.New(slog.NewTextHandler(io.Discard, nil)), 10*time.Millisecond, time.Millisecond)
	}()
	for want := 1; want <= 3; want++ {
		select {
		case got := <-probe.calls:
			if got != want {
				t.Fatalf("call %d, want %d", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("startup/retry/repeat did not run")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("backup did not stop")
	}
}
