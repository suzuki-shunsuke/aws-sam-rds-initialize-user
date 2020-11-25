package entrypoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/sethvargo/go-password/password"
	"github.com/sirupsen/logrus"
	"github.com/suzuki-shunsuke/go-dataeq/dataeq"
)

type Entrypoint struct{}

func (ep Entrypoint) Start(ctx context.Context, ev events.CloudWatchEvent) error {
	logrus.SetFormatter(&logrus.JSONFormatter{})
	cfg := ep.readConfig()
	sess := session.Must(session.NewSession())
	rdsSvc := rds.New(sess)
	secretsManagerSvc := secretsmanager.New(sess)

	if err := ep.start(ctx, ev, cfg, rdsSvc, secretsManagerSvc); err != nil {
		logrus.WithError(err).Error("start")
		return err
	}
	return nil
}

type EventDetail struct {
	SourceIdentifier string
}

func (ep Entrypoint) getDBCluster(
	ctx context.Context, rdsSvc RDSService, ed EventDetail,
) (*rds.DBCluster, error) {
	describeDBClustersOutput, err := rdsSvc.DescribeDBClustersWithContext(ctx, &rds.DescribeDBClustersInput{
		DBClusterIdentifier: aws.String(ed.SourceIdentifier),
	})
	if err != nil {
		return nil, fmt.Errorf("describe clusters: %w", err)
	}

	return getDBCluster(describeDBClustersOutput)
}

func (ep Entrypoint) start( //nolint:funlen
	ctx context.Context, ev events.CloudWatchEvent, cfg Config,
	rdsSvc RDSService, secretsManagerSvc SecretManager,
) error {
	ed := EventDetail{}
	if err := json.Unmarshal(ev.Detail, &ed); err != nil {
		return fmt.Errorf("parse a request body detail: %w", err)
	}
	if ed.SourceIdentifier == "" {
		return errors.New(`request body is invalid. the field detail.SourceIdentifier is missing`)
	}
	logE := logrus.WithFields(logrus.Fields{
		"identifier": ed.SourceIdentifier,
	})
	logE.Info("RDS cluster creation event is hooked")

	dbCluster, err := ep.getDBCluster(ctx, rdsSvc, ed)
	if err != nil {
		return err
	}

	// convert dbCluster to pass Tengo
	dbClusterInterface, err := dataeq.JSON.Convert(dbCluster)
	if err != nil {
		return fmt.Errorf("dataeq.JSON.Convert(*rds.DBCluster): %w", err)
	}

	f, err := ep.filterEvent(ctx, dbClusterInterface, cfg)
	if err != nil {
		return err
	}
	if !f {
		logE.Info("this event is ignored")
		return nil
	}

	masterPW, err := createDBPassword()
	if err != nil {
		return fmt.Errorf("generate a master user password: %w", err)
	}

	driver, err := getDriverFromDBCluster(dbCluster)
	if err != nil {
		return err
	}

	if err := ep.changeMasterPassword(ctx, rdsSvc, ed.SourceIdentifier, masterPW); err != nil {
		return fmt.Errorf("change the master user password: %w", err)
	}
	logE.Info("master user password is changed to a random password")

	connInfo := getDBConnectInfo(dbCluster, driver, masterPW)

	// wait until the password reset is reflected.
	// Without the wait, it would fail to connect to the database.
	timer := time.NewTimer(1 * time.Minute)
	select {
	case <-timer.C:
		return ep.afterMasterUpdated(ctx, secretsManagerSvc, ed.SourceIdentifier, connInfo, cfg, dbClusterInterface)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func getDBConnectInfo(dbCluster *rds.DBCluster, driver, password string) DBConnectInfo {
	return DBConnectInfo{
		Driver:   driver,
		UserName: aws.StringValue(dbCluster.MasterUsername),
		Password: password,
		Port:     aws.Int64Value(dbCluster.Port),
		Host:     aws.StringValue(dbCluster.Endpoint),
		DBName:   aws.StringValue(dbCluster.DatabaseName),
	}
}

func genPasswords(users []string, passwords map[string]string) error {
	for _, a := range users {
		pw, err := createDBPassword()
		if err != nil {
			return fmt.Errorf("generate a application user password: %w", err)
		}
		passwords[a] = pw
	}
	return nil
}

func (ep Entrypoint) afterMasterUpdated(
	ctx context.Context, secretsManagerSvc SecretManager, identifier string,
	connInfo DBConnectInfo, cfg Config, dbCluster interface{},
) error {
	logE := logrus.WithFields(logrus.Fields{
		"identifier": identifier,
	})
	pws := make(map[string]string, len(cfg.Users))
	if err := genPasswords(cfg.Users, pws); err != nil {
		return fmt.Errorf("create passwords: %w", err)
	}

	queries, err := ep.evaluateSQL(ctx, dbCluster, cfg, pws)
	if err != nil {
		return fmt.Errorf("evaluate the parameter SQL: %w", err)
	}

	secrets, err := ep.evaluateSecrets(ctx, dbCluster, cfg, pws)
	if err != nil {
		return fmt.Errorf("evaluate the parameter 'secrets': %w", err)
	}

	if len(queries) != 0 {
		if err := ep.runSQLs(ctx, connInfo, queries); err != nil {
			return fmt.Errorf("run SQL: %w", err)
		}
		logE.Info("SQL has been executed")
	}

	if err := ep.createSecrets(ctx, secretsManagerSvc, secrets); err != nil {
		return fmt.Errorf("create secrets: %w", err)
	}

	logE.Info("store application user's password at AWS Secrets Manager")
	return nil
}

func (ep Entrypoint) createSecrets(
	ctx context.Context, secretsManagerSvc SecretManager, secrets []Secret,
) error {
	for _, secret := range secrets {
		if _, err := secretsManagerSvc.CreateSecretWithContext(ctx, &secretsmanager.CreateSecretInput{
			Name:         aws.String(secret.Name),
			SecretString: aws.String(secret.Secret),
		}); err != nil {
			return fmt.Errorf("create AWS Secrets Manager's secret '%s': %w", secret.Name, err)
		}
	}
	return nil
}

type SecretManager interface {
	CreateSecretWithContext(ctx aws.Context, input *secretsmanager.CreateSecretInput, opts ...request.Option) (*secretsmanager.CreateSecretOutput, error)
}

type DBConnectInfo struct {
	Driver   string
	UserName string
	Password string
	Host     string
	DBName   string
	Port     int64
}

func (connInfo DBConnectInfo) DSN() string {
	// TODO support query string
	return connInfo.Driver + "://" + connInfo.UserName + ":" + connInfo.Password + "@" + connInfo.Host + ":" + strconv.FormatInt(connInfo.Port, 10) + "/" + connInfo.DBName
}

func createDBPassword() (string, error) {
	return password.Generate(32, 10, 0, true, true) //nolint:gomnd
}
