package sqlstore

import (
	"context"
	"time"

	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/models"
)

func (ss *SqlStore) addAPIKeyHandlers() {
	bus.AddHandler("sql", ss.GetApiKeys)
	bus.AddHandler("sql", ss.GetApiKeyById)
	bus.AddHandler("sql", ss.GetApiKeyByName)
	bus.AddHandlerCtx("sql", ss.DeleteApiKeyCtx)
	bus.AddHandler("sql", ss.AddApiKey)
}

func (ss *SqlStore) GetApiKeys(query *models.GetApiKeysQuery) error {
	sess := ss.engine.Limit(100, 0).Where("org_id=? and ( expires IS NULL or expires >= ?)",
		query.OrgId, timeNow().Unix()).Asc("name")
	if query.IncludeExpired {
		sess = ss.engine.Limit(100, 0).Where("org_id=?", query.OrgId).Asc("name")
	}

	query.Result = make([]*models.ApiKey, 0)
	return sess.Find(&query.Result)
}

func (ss *SqlStore) DeleteApiKeyCtx(ctx context.Context, cmd *models.DeleteApiKeyCommand) error {
	return ss.WithDbSession(ctx, func(sess *DBSession) error {
		var rawSql = "DELETE FROM api_key WHERE id=? and org_id=?"
		_, err := sess.Exec(rawSql, cmd.Id, cmd.OrgId)
		return err
	})
}

func (ss *SqlStore) AddApiKey(cmd *models.AddApiKeyCommand) error {
	return ss.inTransaction(func(sess *DBSession) error {
		key := models.ApiKey{OrgId: cmd.OrgId, Name: cmd.Name}
		exists, err := sess.Get(&key)
		if err != nil {
			return err
		}
		if exists {
			return models.ErrDuplicateApiKey
		}

		updated := timeNow()
		var expires *int64 = nil
		if cmd.SecondsToLive > 0 {
			v := updated.Add(time.Second * time.Duration(cmd.SecondsToLive)).Unix()
			expires = &v
		} else if cmd.SecondsToLive < 0 {
			return models.ErrInvalidApiKeyExpiration
		}
		t := models.ApiKey{
			OrgId:   cmd.OrgId,
			Name:    cmd.Name,
			Role:    cmd.Role,
			Key:     cmd.Key,
			Created: updated,
			Updated: updated,
			Expires: expires,
		}

		if _, err := sess.Insert(&t); err != nil {
			return err
		}
		cmd.Result = &t
		return nil
	})
}

func (ss *SqlStore) GetApiKeyById(query *models.GetApiKeyByIdQuery) error {
	var apikey models.ApiKey
	has, err := ss.engine.Id(query.ApiKeyId).Get(&apikey)

	if err != nil {
		return err
	} else if !has {
		return models.ErrInvalidApiKey
	}

	query.Result = &apikey
	return nil
}

func (ss *SqlStore) GetApiKeyByName(query *models.GetApiKeyByNameQuery) error {
	var apikey models.ApiKey
	has, err := ss.engine.Where("org_id=? AND name=?", query.OrgId, query.KeyName).Get(&apikey)

	if err != nil {
		return err
	} else if !has {
		return models.ErrInvalidApiKey
	}

	query.Result = &apikey
	return nil
}
