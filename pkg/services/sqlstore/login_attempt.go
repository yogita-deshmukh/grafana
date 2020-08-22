package sqlstore

import (
	"strconv"
	"time"

	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/models"
)

var getTimeNow = time.Now

func (ss *SqlStore) addLoginAttemptHandlers() {
	bus.AddHandler("sql", ss.CreateLoginAttempt)
	bus.AddHandler("sql", ss.DeleteOldLoginAttempts)
	bus.AddHandler("sql", ss.GetUserLoginAttemptCount)
}

func (ss *SqlStore) CreateLoginAttempt(cmd *models.CreateLoginAttemptCommand) error {
	return ss.inTransaction(func(sess *DBSession) error {
		loginAttempt := models.LoginAttempt{
			Username:  cmd.Username,
			IpAddress: cmd.IpAddress,
			Created:   getTimeNow().Unix(),
		}

		if _, err := sess.Insert(&loginAttempt); err != nil {
			return err
		}

		cmd.Result = loginAttempt

		return nil
	})
}

func (ss *SqlStore) DeleteOldLoginAttempts(cmd *models.DeleteOldLoginAttemptsCommand) error {
	return ss.inTransaction(func(sess *DBSession) error {
		var maxId int64
		sql := "SELECT max(id) as id FROM login_attempt WHERE created < ?"
		result, err := sess.Query(sql, cmd.OlderThan.Unix())
		if err != nil {
			return err
		}
		// nolint: gosimple
		if result == nil || len(result) == 0 || result[0] == nil {
			return nil
		}

		maxId = toInt64(result[0]["id"])

		if maxId == 0 {
			return nil
		}

		sql = "DELETE FROM login_attempt WHERE id <= ?"

		if result, err := sess.Exec(sql, maxId); err != nil {
			return err
		} else if cmd.DeletedRows, err = result.RowsAffected(); err != nil {
			return err
		}

		return nil
	})
}

func (ss *SqlStore) GetUserLoginAttemptCount(query *models.GetUserLoginAttemptCountQuery) error {
	loginAttempt := new(models.LoginAttempt)
	total, err := ss.engine.
		Where("username = ?", query.Username).
		And("created >= ?", query.Since.Unix()).
		Count(loginAttempt)
	if err != nil {
		return err
	}

	query.Result = total
	return nil
}

func toInt64(i interface{}) int64 {
	switch i := i.(type) {
	case []byte:
		n, _ := strconv.ParseInt(string(i), 10, 64)
		return n
	case int:
		return int64(i)
	case int64:
		return i
	}
	return 0
}
