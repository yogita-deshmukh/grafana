package sqlstore

import (
	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/models"
)

func (ss *SqlStore) addStarHandlers() {
	bus.AddHandler("sql", ss.StarDashboard)
	bus.AddHandler("sql", ss.UnstarDashboard)
	bus.AddHandler("sql", ss.GetUserStars)
	bus.AddHandler("sql", ss.IsStarredByUser)
}

func (ss *SqlStore) IsStarredByUser(query *models.IsStarredByUserQuery) error {
	rawSql := "SELECT 1 from star where user_id=? and dashboard_id=?"
	results, err := ss.engine.Query(rawSql, query.UserId, query.DashboardId)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return nil
	}

	query.Result = true

	return nil
}

func (ss *SqlStore) StarDashboard(cmd *models.StarDashboardCommand) error {
	if cmd.DashboardId == 0 || cmd.UserId == 0 {
		return models.ErrCommandValidationFailed
	}

	return ss.inTransaction(func(sess *DBSession) error {
		entity := models.Star{
			UserId:      cmd.UserId,
			DashboardId: cmd.DashboardId,
		}

		_, err := sess.Insert(&entity)
		return err
	})
}

func (ss *SqlStore) UnstarDashboard(cmd *models.UnstarDashboardCommand) error {
	if cmd.DashboardId == 0 || cmd.UserId == 0 {
		return models.ErrCommandValidationFailed
	}

	return ss.inTransaction(func(sess *DBSession) error {
		var rawSql = "DELETE FROM star WHERE user_id=? and dashboard_id=?"
		_, err := sess.Exec(rawSql, cmd.UserId, cmd.DashboardId)
		return err
	})
}

func (ss *SqlStore) GetUserStars(query *models.GetUserStarsQuery) error {
	var stars = make([]models.Star, 0)
	err := ss.engine.Where("user_id=?", query.UserId).Find(&stars)
	if err != nil {
		return err
	}

	query.Result = make(map[int64]bool)
	for _, star := range stars {
		query.Result[star.DashboardId] = true
	}

	return nil
}
