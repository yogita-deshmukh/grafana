package sqlstore

import (
	"github.com/grafana/grafana/pkg/models"
)

func (ss *SqlStore) GetDBHealthQuery(query *models.GetDBHealthQuery) error {
	return ss.engine.Ping()
}
