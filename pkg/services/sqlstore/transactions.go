package sqlstore

import (
	"context"
	"time"

	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/util/errutil"
	"github.com/mattn/go-sqlite3"
)

// WithTransactionalDbSession calls the callback with a session within a transaction.
func (ss *SqlStore) WithTransactionalDbSession(ctx context.Context, callback dbTransactionFunc) error {
	return ss.inTransactionWithRetryCtx(ctx, callback, 0)
}

func (ss *SqlStore) InTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return ss.inTransactionWithRetryCtx(ctx, func(sess *DBSession) error {
		withValue := context.WithValue(ctx, ContextSessionKey{}, sess)
		return fn(withValue)
	}, 0)
}

func (ss *SqlStore) inTransactionWithRetryCtx(ctx context.Context, callback dbTransactionFunc, retry int) error {
	sess, err := ss.startSession(ctx, true)
	if err != nil {
		return err
	}

	defer sess.Close()

	err = callback(sess)
	// special handling of database locked errors for sqlite, then we can retry 5 times
	if sqlError, ok := err.(sqlite3.Error); ok && retry < 5 && sqlError.Code ==
		sqlite3.ErrLocked || sqlError.Code == sqlite3.ErrBusy {
		if rollErr := sess.Rollback(); rollErr != nil {
			return errutil.Wrapf(err, "Rolling back transaction due to error failed: %s", rollErr)
		}

		time.Sleep(time.Millisecond * time.Duration(10))
		sqlog.Info("Database locked, sleeping then retrying", "error", err, "retry", retry)
		return ss.inTransactionWithRetryCtx(context.Background(), callback, retry+1)
	}
	if err != nil {
		if rollErr := sess.Rollback(); rollErr != nil {
			return errutil.Wrapf(err, "Rolling back transaction due to error failed: %s", rollErr)
		}
		return err
	}

	if err := sess.Commit(); err != nil {
		return err
	}

	if len(sess.events) > 0 {
		for _, e := range sess.events {
			if err = bus.Publish(e); err != nil {
				log.Errorf(3, "Failed to publish event after commit. error: %v", err)
			}
		}
	}

	return nil
}

func (ss *SqlStore) inTransaction(callback dbTransactionFunc) error {
	return ss.inTransactionWithRetryCtx(context.Background(), callback, 0)
}

func (ss *SqlStore) inTransactionCtx(ctx context.Context, callback dbTransactionFunc) error {
	return ss.inTransactionWithRetryCtx(ctx, callback, 0)
}
