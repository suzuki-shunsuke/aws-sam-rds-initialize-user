package entrypoint

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/d5/tengo/v2"
	"github.com/d5/tengo/v2/stdlib"
	_ "github.com/lib/pq"
	"github.com/sethvargo/go-password/password"
	"github.com/sirupsen/logrus"
	"github.com/suzuki-shunsuke/aws-sam-rds-initialize-user/pkg/constant"
	"github.com/suzuki-shunsuke/go-dataeq/dataeq"
)

type Entrypoint struct{}

type Config struct {
	EventFilter string
}

func (ep Entrypoint) Start(ctx context.Context, ev events.CloudWatchEvent) error {
	logrus.SetFormatter(&logrus.JSONFormatter{})
	cfg := Config{
		EventFilter: os.Getenv("EVENT_FILTER"),
	}
	if err := ep.start(ctx, ev, cfg); err != nil {
		logrus.WithError(err).Error("start")
		return err
	}
	return nil
}

type EventDetail struct {
	SourceIdentifier string
}

func (ep Entrypoint) start(ctx context.Context, ev events.CloudWatchEvent, cfg Config) error { //nolint:funlen
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

	sess := session.Must(session.NewSession())
	rdsSvc := rds.New(sess)
	// describe the DB
	describeDBClustersOutput, err := rdsSvc.DescribeDBClustersWithContext(ctx, &rds.DescribeDBClustersInput{
		DBClusterIdentifier: aws.String(ed.SourceIdentifier),
	})
	if err != nil {
		return fmt.Errorf("describe clusters: %w", err)
	}

	dbCluster, err := getDBCluster(describeDBClustersOutput)
	if err != nil {
		return err
	}

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

	if err := ep.changeMasterPassword(ctx, rdsSvc, ed.SourceIdentifier, masterPW); err != nil {
		return fmt.Errorf("change the master user password: %w", err)
	}
	logE.Info("master user password is changed to a random password")

	driver, err := getDriverFromDBCluster(dbCluster)
	if err != nil {
		return err
	}

	// connect to DB
	connInfo := DBConnectInfo{
		Driver:   driver,
		UserName: aws.StringValue(dbCluster.MasterUsername),
		Password: masterPW,
		Port:     aws.Int64Value(dbCluster.Port),
		Host:     aws.StringValue(dbCluster.Endpoint),
		DBName:   aws.StringValue(dbCluster.DatabaseName),
	}

	// wait until the password reset is reflected.
	// Without the wait, it would fail to connect to the database.
	timer := time.NewTimer(1 * time.Minute)
	select {
	case <-timer.C:
		return ep.afterMasterUpdated(ctx, sess, ed.SourceIdentifier, connInfo)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (ep Entrypoint) filterEvent(ctx context.Context, dbCluster interface{}, cfg Config) (bool, error) {
	if cfg.EventFilter == "" {
		return true, nil
	}
	script := tengo.NewScript([]byte(cfg.EventFilter))
	script.SetImports(stdlib.GetModuleMap(stdlib.AllModuleNames()...))
	if err := script.Add("DBCluster", dbCluster); err != nil {
		return false, fmt.Errorf("add DBCluster to the Tengo script: %w", err)
	}
	compiled, err := script.RunContext(ctx)
	if err != nil {
		return false, fmt.Errorf("compile and run the Tengo script: %w", err)
	}
	if !compiled.IsDefined(constant.Result) {
		return false, constant.ErrNoBoolVariable
	}
	v := compiled.Get(constant.Result)
	if t := v.ValueType(); t != "bool" {
		return false, errors.New(`the type of the variable "result" should be bool, but actually ` + t)
	}
	return v.Bool(), nil
}

func (ep Entrypoint) afterMasterUpdated(ctx context.Context, sess client.ConfigProvider, identifier string, connInfo DBConnectInfo) error { //nolint:funlen
	logE := logrus.WithFields(logrus.Fields{
		"identifier": identifier,
	})
	db, err := sql.Open(connInfo.Driver, connInfo.DSN())
	if err != nil {
		return fmt.Errorf("connect to the database: %w", err)
	}

	// create a user password
	pw, err := createDBPassword()
	if err != nil {
		return fmt.Errorf("generate a application user password: %w", err)
	}

	// TODO configure user name
	// create a user
	userName := "app"
	// TODO support MySQL
	// TODO support to customize SQL
	if err := createPostgresUser(ctx, db, ParamsCreateUser{
		DBName:   connInfo.DBName,
		UserName: userName,
		Password: pw,
	}); err != nil {
		return err
	}
	logE.Info("an application user has been created")

	secret, err := json.Marshal(map[string]string{
		"username": userName,
		"password": pw,
	})
	if err != nil {
		return fmt.Errorf("marshal a secret: %w", err)
	}

	// create AWS Secrets Manager's secret for user password
	// TODO configure secret name
	// TODO support to customize secret
	secretsManagerSvc := secretsmanager.New(sess)
	if _, err := secretsManagerSvc.CreateSecretWithContext(ctx, &secretsmanager.CreateSecretInput{
		Name:         aws.String("rds-app-user-password-" + identifier),
		SecretString: aws.String(string(secret)),
	}); err != nil {
		return fmt.Errorf("store application user's password at AWS Secrets Manager: %w", err)
	}
	logE.Info("store application user's password at AWS Secrets Manager")
	return nil
}

type ParamsCreateUser struct {
	DBName   string
	UserName string
	Password string
}

func createPostgresUser(ctx context.Context, db *sql.DB, params ParamsCreateUser) error {
	createSQL := `CREATE ROLE ` + params.UserName + ` WITH LOGIN PASSWORD '` + params.Password + `'`
	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		// the error message includes a password, but there is no problem because it failed to set this password.
		return fmt.Errorf("create a role: %s: %w", createSQL, err)
	}
	alterSQL := `ALTER DATABASE ` + params.DBName + ` OWNER TO ` + params.UserName
	if _, err := db.ExecContext(ctx, alterSQL); err != nil {
		return fmt.Errorf("give the user the permission of the database: %s: %w", alterSQL, err)
	}

	return nil
}

func getDBCluster(output *rds.DescribeDBClustersOutput) (*rds.DBCluster, error) {
	if output == nil {
		return nil, errors.New(`describe DB clusters output is nil`)
	}
	dbClusters := output.DBClusters
	if len(dbClusters) != 1 {
		return nil, errors.New("the number of DB clusters must be 1: " + strconv.Itoa(len(dbClusters)))
	}
	dbCluster := dbClusters[0]
	if dbCluster == nil {
		return nil, errors.New("db cluster is nil")
	}
	return dbCluster, nil
}

func getDriverFromDBCluster(dbCluster *rds.DBCluster) (string, error) {
	driver, err := getDriver(aws.StringValue(dbCluster.Engine))
	if err != nil {
		return "", fmt.Errorf("get a DB driver type from the DB cluster engine: %w", err)
	}
	return driver, nil
}

func getDriver(engine string) (string, error) {
	if strings.Contains(engine, "postgresql") {
		return "postgres", nil
	}
	if strings.Contains(engine, "mysql") {
		return "mysql", nil
	}
	return "", errors.New("unsupported RDS engine: " + engine)
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

type RDSService interface {
	ModifyDBClusterWithContext(ctx aws.Context, input *rds.ModifyDBClusterInput, opts ...request.Option) (*rds.ModifyDBClusterOutput, error)
	DescribeDBClustersWithContext(ctx aws.Context, input *rds.DescribeDBClustersInput, opts ...request.Option) (*rds.DescribeDBClustersOutput, error)
}

func createDBPassword() (string, error) {
	return password.Generate(32, 10, 0, true, true) //nolint:gomnd
}

func (ep Entrypoint) changeMasterPassword(ctx context.Context, rdsSvc RDSService, identifier, pw string) error {
	if _, err := rdsSvc.ModifyDBClusterWithContext(ctx, &rds.ModifyDBClusterInput{
		ApplyImmediately:    aws.Bool(true),
		DBClusterIdentifier: aws.String(identifier),
		MasterUserPassword:  aws.String(pw),
	}); err != nil {
		return fmt.Errorf("modify a DB cluster by AWS API: %w", err)
	}
	return nil
}
