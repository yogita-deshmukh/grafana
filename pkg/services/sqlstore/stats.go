package sqlstore

import (
	"context"
	"time"

	"github.com/grafana/grafana/pkg/models"
)

var activeUserTimeLimit = time.Hour * 24 * 30

func (ss *SqlStore) GetAlertNotifiersUsageStats(ctx context.Context, query *models.GetAlertNotifierUsageStatsQuery) error {
	var rawSql = `SELECT COUNT(*) AS count, type FROM ` + ss.Dialect.Quote("alert_notification") + ` GROUP BY type`
	query.Result = make([]*models.NotifierUsageStats, 0)
	err := ss.engine.SQL(rawSql).Find(&query.Result)
	return err
}

func (ss *SqlStore) GetDataSourceStats(query *models.GetDataSourceStatsQuery) error {
	var rawSql = `SELECT COUNT(*) AS count, type FROM ` + ss.Dialect.Quote("data_source") + ` GROUP BY type`
	query.Result = make([]*models.DataSourceStats, 0)
	err := ss.engine.SQL(rawSql).Find(&query.Result)
	return err
}

func (ss *SqlStore) GetDataSourceAccessStats(query *models.GetDataSourceAccessStatsQuery) error {
	var rawSql = `SELECT COUNT(*) AS count, type, access FROM ` + ss.Dialect.Quote("data_source") + ` GROUP BY type, access`
	query.Result = make([]*models.DataSourceAccessStats, 0)
	err := ss.engine.SQL(rawSql).Find(&query.Result)
	return err
}

func (ss *SqlStore) GetSystemStats(query *models.GetSystemStatsQuery) error {
	dialect := ss.Dialect

	sb := &SqlBuilder{
		dialect: ss.Dialect,
	}
	sb.Write("SELECT ")
	sb.Write(`(SELECT COUNT(*) FROM ` + dialect.Quote("user") + `) AS users,`)
	sb.Write(`(SELECT COUNT(*) FROM ` + dialect.Quote("org") + `) AS orgs,`)
	sb.Write(`(SELECT COUNT(*) FROM ` + dialect.Quote("dashboard") + `) AS dashboards,`)
	sb.Write(`(SELECT COUNT(*) FROM ` + dialect.Quote("data_source") + `) AS datasources,`)
	sb.Write(`(SELECT COUNT(*) FROM ` + dialect.Quote("star") + `) AS stars,`)
	sb.Write(`(SELECT COUNT(*) FROM ` + dialect.Quote("playlist") + `) AS playlists,`)
	sb.Write(`(SELECT COUNT(*) FROM ` + dialect.Quote("alert") + `) AS alerts,`)

	activeUserDeadlineDate := time.Now().Add(-activeUserTimeLimit)
	sb.Write(`(SELECT COUNT(*) FROM `+dialect.Quote("user")+` WHERE last_seen_at > ?) AS active_users,`, activeUserDeadlineDate)

	sb.Write(`(SELECT COUNT(id) FROM `+dialect.Quote("dashboard")+` WHERE is_folder = ?) AS folders,`, dialect.BooleanStr(true))

	sb.Write(`(
		SELECT COUNT(acl.id)
		FROM `+dialect.Quote("dashboard_acl")+` AS acl
			INNER JOIN `+dialect.Quote("dashboard")+` AS d
			ON d.id = acl.dashboard_id
		WHERE d.is_folder = ?
	) AS dashboard_permissions,`, dialect.BooleanStr(false))

	sb.Write(`(
		SELECT COUNT(acl.id)
		FROM `+dialect.Quote("dashboard_acl")+` AS acl
			INNER JOIN `+dialect.Quote("dashboard")+` AS d
			ON d.id = acl.dashboard_id
		WHERE d.is_folder = ?
	) AS folder_permissions,`, dialect.BooleanStr(true))

	sb.Write(`(SELECT COUNT(id) FROM ` + dialect.Quote("dashboard_provisioning") + `) AS provisioned_dashboards,`)
	sb.Write(`(SELECT COUNT(id) FROM ` + dialect.Quote("dashboard_snapshot") + `) AS snapshots,`)
	sb.Write(`(SELECT COUNT(id) FROM ` + dialect.Quote("dashboard_version") + `) AS dashboard_versions,`)
	sb.Write(`(SELECT COUNT(id) FROM ` + dialect.Quote("annotation") + `) AS annotations,`)
	sb.Write(`(SELECT COUNT(id) FROM ` + dialect.Quote("team") + `) AS teams,`)
	sb.Write(`(SELECT COUNT(id) FROM ` + dialect.Quote("user_auth_token") + `) AS auth_tokens,`)

	sb.Write(ss.roleCounterSQL("Viewer", "viewers", false)+`,`, activeUserDeadlineDate)
	sb.Write(ss.roleCounterSQL("Editor", "editors", false)+`,`, activeUserDeadlineDate)
	sb.Write(ss.roleCounterSQL("Admin", "admins", false)+``, activeUserDeadlineDate)

	var stats models.SystemStats
	_, err := ss.engine.SQL(sb.GetSqlString(), sb.params...).Get(&stats)
	if err != nil {
		return err
	}

	query.Result = &stats

	return nil
}

func (ss *SqlStore) roleCounterSQL(role string, alias string, onlyActive bool) string {
	dialect := ss.Dialect
	var sqlQuery = `
		(
			SELECT COUNT(DISTINCT u.id)
			FROM ` + dialect.Quote("user") + ` AS u, org_user
			WHERE u.last_seen_at > ? AND ( org_user.user_id=u.id AND org_user.role='` + role + `' )
		) AS active_` + alias

	if !onlyActive {
		sqlQuery += `,
		(
			SELECT COUNT(DISTINCT u.id)
			FROM ` + dialect.Quote("user") + ` AS u, org_user
			WHERE ( org_user.user_id=u.id AND org_user.role='` + role + `' )
		) AS ` + alias
	}

	return sqlQuery
}

func (ss *SqlStore) GetAdminStats(query *models.GetAdminStatsQuery) error {
	activeEndDate := time.Now().Add(-activeUserTimeLimit)
	dialect := ss.Dialect

	var rawSql = `SELECT
		(
			SELECT COUNT(*)
			FROM ` + dialect.Quote("org") + `
		) AS orgs,
		(
			SELECT COUNT(*)
			FROM ` + dialect.Quote("dashboard") + `
		) AS dashboards,
		(
			SELECT COUNT(*)
			FROM ` + dialect.Quote("dashboard_snapshot") + `
		) AS snapshots,
		(
			SELECT COUNT( DISTINCT ( ` + dialect.Quote("term") + ` ))
			FROM ` + dialect.Quote("dashboard_tag") + `
		) AS tags,
		(
			SELECT COUNT(*)
			FROM ` + dialect.Quote("data_source") + `
		) AS datasources,
		(
			SELECT COUNT(*)
			FROM ` + dialect.Quote("playlist") + `
		) AS playlists,
		(
			SELECT COUNT(*)
			FROM ` + dialect.Quote("star") + `
		) AS stars,
		(
			SELECT COUNT(*)
			FROM ` + dialect.Quote("alert") + `
		) AS alerts,
		(
			SELECT COUNT(*)
			FROM ` + dialect.Quote("user") + `
		) AS users,
		(
			SELECT COUNT(*)
			FROM ` + dialect.Quote("user") + ` WHERE last_seen_at > ?
		) AS active_users,
		` + ss.roleCounterSQL("Admin", "admins", false) + `,
		` + ss.roleCounterSQL("Editor", "editors", false) + `,
		` + ss.roleCounterSQL("Viewer", "viewers", false) + `,
		(
			SELECT COUNT(*)
			FROM ` + dialect.Quote("user_auth_token") + ` WHERE rotated_at > ?
		) AS active_sessions`

	var stats models.AdminStats
	_, err := ss.engine.SQL(rawSql, activeEndDate, activeEndDate, activeEndDate, activeEndDate, activeEndDate.Unix()).Get(&stats)
	if err != nil {
		return err
	}

	query.Result = &stats
	return nil
}

func (ss *SqlStore) GetSystemUserCountStats(ctx context.Context, query *models.GetSystemUserCountStatsQuery) error {
	return ss.WithDbSession(ctx, func(sess *DBSession) error {
		var rawSql = `SELECT COUNT(id) AS Count FROM ` + ss.Dialect.Quote("user")
		var stats models.SystemUserCountStats
		if _, err := sess.SQL(rawSql).Get(&stats); err != nil {
			return err
		}

		query.Result = &stats

		return nil
	})
}

func (ss *SqlStore) GetActiveUserStats(query *models.GetActiveUserStatsQuery) error {
	activeUserDeadlineDate := time.Now().Add(-activeUserTimeLimit)
	sb := &SqlBuilder{
		dialect: ss.Dialect,
	}

	sb.Write(`SELECT `)
	sb.Write(`(SELECT COUNT(*) FROM `+ss.Dialect.Quote("user")+` WHERE last_seen_at > ?) AS active_users,`, activeUserDeadlineDate)
	sb.Write(ss.roleCounterSQL("Viewer", "viewers", true)+`,`, activeUserDeadlineDate)
	sb.Write(ss.roleCounterSQL("Editor", "editors", true)+`,`, activeUserDeadlineDate)
	sb.Write(ss.roleCounterSQL("Admin", "admins", true)+``, activeUserDeadlineDate)

	var stats models.ActiveUserStats
	_, err := ss.engine.SQL(sb.GetSqlString(), sb.params...).Get(&stats)
	if err != nil {
		return err
	}

	query.Result = &stats

	return nil
}
