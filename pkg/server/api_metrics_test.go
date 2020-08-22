package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatch/cloudwatchiface"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs/cloudwatchlogsiface"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/grafana/grafana/pkg/tsdb"
	"github.com/grafana/grafana/pkg/tsdb/cloudwatch"

	cwapi "github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/grafana/grafana/pkg/api/dtos"
	"github.com/grafana/grafana/pkg/components/simplejson"
	"github.com/grafana/grafana/pkg/infra/fs"
	"github.com/grafana/grafana/pkg/models"
	"github.com/grafana/grafana/pkg/services/sqlstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/ini.v1"
)

var addr string
var sqlStore *sqlstore.SqlStore

func main(m *testing.M) int {
	grafDir, cfgPath, err := createGrafDir()
	if err != nil {
		log.Fatal().Err(err).Msgf("Failed to create Grafana dir")
	}
	defer func() {
		if err := os.RemoveAll(grafDir); err != nil {
			log.Warn().Err(err).Msgf("Failed to remove Grafana dir %q", grafDir)
		}
		log.Info().Msgf("Removed Grafana dir %q", grafDir)
	}()

	sqlStore, err = sqlstore.New()
	if err != nil {
		log.Fatal().Err(err).Msgf("Failed to set up database")
	}

	var server *Server
	server, addr, err = startGrafana(grafDir, cfgPath, sqlStore)
	if err != nil {
		log.Fatal().Err(err).Msgf("Failed to start Grafana")
	}
	defer server.Shutdown("")

	return m.Run()
}

func TestMain(m *testing.M) {
	os.Exit(main(m))
}

func TestQueryCloudWatchMetrics(t *testing.T) {
	origNewCWClient := cloudwatch.NewCWClient
	t.Cleanup(func() {
		cloudwatch.NewCWClient = origNewCWClient
	})

	var client cloudwatch.FakeCWClient
	cloudwatch.NewCWClient = func(sess *session.Session) cloudwatchiface.CloudWatchAPI {
		return client
	}

	t.Run("Custom metrics", func(t *testing.T) {
		resetDatabase(t)

		client = cloudwatch.FakeCWClient{
			Metrics: []*cwapi.Metric{
				{
					MetricName: aws.String("Test_MetricName"),
					Dimensions: []*cwapi.Dimension{
						{
							Name: aws.String("Test_DimensionName"),
						},
					},
				},
			},
		}

		req := dtos.MetricRequest{
			Queries: []*simplejson.Json{
				simplejson.NewFromAny(map[string]interface{}{
					"type":         "metricFindQuery",
					"subtype":      "metrics",
					"region":       "us-east-1",
					"namespace":    "custom",
					"datasourceId": 1,
				}),
			},
		}
		tr := makeCWRequest(t, req, addr)

		assert.Equal(t, tsdb.Response{
			Results: map[string]*tsdb.QueryResult{
				"A": {
					RefId: "A",
					Meta: simplejson.NewFromAny(map[string]interface{}{
						"rowCount": float64(1),
					}),
					Tables: []*tsdb.Table{
						{
							Columns: []tsdb.TableColumn{
								{
									Text: "text",
								},
								{
									Text: "value",
								},
							},
							Rows: []tsdb.RowValues{
								{
									"Test_MetricName",
									"Test_MetricName",
								},
							},
						},
					},
				},
			},
		}, tr)
	})
}

func TestQueryCloudWatchLogs(t *testing.T) {
	origNewCWLogsClient := cloudwatch.NewCWLogsClient
	t.Cleanup(func() {
		cloudwatch.NewCWLogsClient = origNewCWLogsClient
	})

	var client cloudwatch.FakeCWLogsClient
	cloudwatch.NewCWLogsClient = func(sess *session.Session) cloudwatchlogsiface.CloudWatchLogsAPI {
		return client
	}

	t.Run("Describe log groups", func(t *testing.T) {
		resetDatabase(t)

		client = cloudwatch.FakeCWLogsClient{}

		req := dtos.MetricRequest{
			Queries: []*simplejson.Json{
				simplejson.NewFromAny(map[string]interface{}{
					"type":         "logAction",
					"subtype":      "DescribeLogGroups",
					"region":       "us-east-1",
					"datasourceId": 1,
				}),
			},
		}
		tr := makeCWRequest(t, req, addr)

		dataFrames := tsdb.NewDecodedDataFrames(data.Frames{
			&data.Frame{
				Name: "logGroups",
				Fields: []*data.Field{
					data.NewField("logGroupName", nil, []*string{}),
				},
				Meta: &data.FrameMeta{
					PreferredVisualization: "logs",
				},
			},
		})
		// Have to call this so that dataFrames.encoded is non-nil, for the comparison
		// In the future we should use gocmp instead and ignore this field
		_, err := dataFrames.Encoded()
		require.NoError(t, err)
		assert.Equal(t, tsdb.Response{
			Results: map[string]*tsdb.QueryResult{
				"A": {
					RefId:      "A",
					Dataframes: dataFrames,
				},
			},
		}, tr)
	})
}

func makeCWRequest(t *testing.T, req dtos.MetricRequest, addr string) tsdb.Response {
	t.Helper()

	buf := bytes.Buffer{}
	enc := json.NewEncoder(&buf)
	err := enc.Encode(&req)
	require.NoError(t, err)
	resp, err := http.Post(fmt.Sprintf("http://%s/api/ds/query", addr), "application/json", &buf)
	require.NoError(t, err)
	require.NotNil(t, resp)
	t.Cleanup(func() { resp.Body.Close() })

	buf = bytes.Buffer{}
	_, err = io.Copy(&buf, resp.Body)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var tr tsdb.Response
	err = json.Unmarshal(buf.Bytes(), &tr)
	require.NoError(t, err)

	return tr
}

func createGrafDir() (string, string, error) {
	tmpDir, err := ioutil.TempDir("", "")
	if err != nil {
		return "", "", err
	}

	rootDir := filepath.Join("..", "..")

	cfgDir := filepath.Join(tmpDir, "conf")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		return "", "", err
	}
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", "", err
	}
	logsDir := filepath.Join(tmpDir, "logs")
	pluginsDir := filepath.Join(tmpDir, "plugins")
	publicDir := filepath.Join(tmpDir, "public")
	if err := os.MkdirAll(publicDir, 0755); err != nil {
		return "", "", err
	}
	emailsDir := filepath.Join(publicDir, "emails")
	if err := fs.CopyRecursive(filepath.Join(rootDir, "public", "emails"), emailsDir); err != nil {
		return "", "", err
	}
	provDir := filepath.Join(cfgDir, "provisioning")
	provDSDir := filepath.Join(provDir, "datasources")
	if err := os.MkdirAll(provDSDir, 0755); err != nil {
		return "", "", err
	}
	provNotifiersDir := filepath.Join(provDir, "notifiers")
	if err := os.MkdirAll(provNotifiersDir, 0755); err != nil {
		return "", "", err
	}
	provPluginsDir := filepath.Join(provDir, "plugins")
	if err := os.MkdirAll(provPluginsDir, 0755); err != nil {
		return "", "", err
	}
	provDashboardsDir := filepath.Join(provDir, "dashboards")
	if err := os.MkdirAll(provDashboardsDir, 0755); err != nil {
		return "", "", err
	}

	cfg := ini.Empty()
	dfltSect := cfg.Section("")
	if _, err := dfltSect.NewKey("app_mode", "development"); err != nil {
		return "", "", err
	}

	pathsSect, err := cfg.NewSection("paths")
	if err != nil {
		return "", "", err
	}
	if _, err := pathsSect.NewKey("data", dataDir); err != nil {
		return "", "", err
	}
	if _, err := pathsSect.NewKey("logs", logsDir); err != nil {
		return "", "", err
	}
	if _, err := pathsSect.NewKey("plugins", pluginsDir); err != nil {
		return "", "", err
	}

	logSect, err := cfg.NewSection("log")
	if err != nil {
		return "", "", err
	}
	if _, err := logSect.NewKey("level", "debug"); err != nil {
		return "", "", err
	}

	serverSect, err := cfg.NewSection("server")
	if err != nil {
		return "", "", err
	}
	if _, err := serverSect.NewKey("port", "0"); err != nil {
		return "", "", err
	}

	anonSect, err := cfg.NewSection("auth.anonymous")
	if err != nil {
		return "", "", err
	}
	if _, err := anonSect.NewKey("enabled", "true"); err != nil {
		return "", "", err
	}

	cfgPath := filepath.Join(cfgDir, "test.ini")
	if err := cfg.SaveTo(cfgPath); err != nil {
		return "", "", err
	}

	if err := fs.CopyFile(filepath.Join(rootDir, "conf", "defaults.ini"), filepath.Join(cfgDir, "defaults.ini")); err != nil {
		return "", "", err
	}

	return tmpDir, cfgPath, nil
}

func startGrafana(grafDir, cfgPath string, sqlStore *sqlstore.SqlStore) (*Server, string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	server, err := New(Config{
		ConfigFile: cfgPath,
		HomePath:   grafDir,
		Listener:   listener,
		SQLStore:   sqlStore,
	})
	if err != nil {
		return nil, "", err
	}

	go func() {
		if err := server.Run(); err != nil {
			log.Error().Err(err).Msgf("Server exited uncleanly")
		}
	}()

	// Wait for Grafana to be ready
	addr := listener.Addr().String()
	resp, err := http.Get(fmt.Sprintf("http://%s/api/health", addr))
	if err != nil {
		return nil, "", err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("got error response: %s", resp.Status)
	}

	log.Debug().Msgf("Grafana is listening on %s", addr)

	return server, addr, nil
}

func resetDatabase(t *testing.T) {
	t.Helper()

	t.Log("Cleaning DB")

	type tcount struct {
		Count int64
	}

	require.NotNil(t, sqlStore)
	err := sqlStore.Reset()
	require.NoError(t, err)

	t.Log("Database was reset")
	err = sqlStore.WithDbSession(context.Background(), func(session *sqlstore.DBSession) error {
		resp := make([]*tcount, 0)
		if err := session.SQL("select count(id) from data_source").Find(&resp); err != nil {
			return err
		}
		t.Log("The number of sources", "num", resp[0].Count)
		return nil
	})
	require.NoError(t, err)

	err = sqlStore.WithDbSession(context.Background(), func(sess *sqlstore.DBSession) error {
		_, err := sess.Insert(&models.DataSource{
			Id:      1,
			OrgId:   1,
			Name:    "Test",
			Type:    "cloudwatch",
			Created: time.Now(),
			Updated: time.Now(),
		})
		return err
	})
	require.NoError(t, err)
}
