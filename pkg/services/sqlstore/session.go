package sqlstore

import (
	"context"
	"reflect"

	"github.com/grafana/grafana/pkg/services/sqlstore/migrator"
	"xorm.io/xorm"
)

type DBSession struct {
	*xorm.Session
	events  []interface{}
	dialect migrator.Dialect
}

type dbTransactionFunc func(sess *DBSession) error

func (sess *DBSession) publishAfterCommit(msg interface{}) {
	sess.events = append(sess.events, msg)
}

// NewSession returns a new DBSession
func (ss *SqlStore) NewSession() *DBSession {
	return &DBSession{
		Session: ss.engine.NewSession(),
		dialect: ss.Dialect,
	}
}

func (ss *SqlStore) startSession(ctx context.Context, beginTran bool) (*DBSession, error) {
	value := ctx.Value(ContextSessionKey{})
	var sess *DBSession
	sess, ok := value.(*DBSession)
	if ok {
		return sess, nil
	}

	newSess := &DBSession{
		Session: ss.engine.NewSession(),
		dialect: ss.Dialect,
	}
	if beginTran {
		if err := newSess.Begin(); err != nil {
			return nil, err
		}
	}

	return newSess, nil
}

// WithDbSession calls the callback with an session attached to the context.
func (ss *SqlStore) WithDbSession(ctx context.Context, callback dbTransactionFunc) error {
	sess, err := ss.startSession(ctx, false)
	if err != nil {
		return err
	}
	defer sess.Close()

	return callback(sess)
}

func (sess *DBSession) InsertId(bean interface{}) (int64, error) {
	table := sess.DB().Mapper.Obj2Table(getTypeName(bean))

	if err := sess.dialect.PreInsertId(table, sess.Session); err != nil {
		return 0, err
	}
	id, err := sess.Session.InsertOne(bean)
	if err != nil {
		return 0, err
	}
	if err := sess.dialect.PostInsertId(table, sess.Session); err != nil {
		return 0, err
	}

	return id, nil
}

func getTypeName(bean interface{}) (res string) {
	t := reflect.TypeOf(bean)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}
