package state

import (
	"context"
	"github.com/gofrs/flock"
	"path/filepath"
	"time"
)

// WaitLock serializes operations across plugin processes until unlock is called.
// It waits for the lock or context cancellation, including across Herdr calls
// that deliberately happen outside short SQLite transactions.
func (s *Store) WaitLock(ctx context.Context, name string) (func(), error) {
	l := flock.New(filepath.Join(s.dir, name+".lock"))
	ok, err := l.TryLockContext(ctx, 25*time.Millisecond)
	if err != nil || !ok {
		l.Close()
		if err != nil {
			return nil, err
		}
		return nil, ctx.Err()
	}
	return func() { _ = l.Close() }, nil
}
