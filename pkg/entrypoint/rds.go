package entrypoint

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/rds"
)

type RDSService interface {
	ModifyDBClusterWithContext(ctx aws.Context, input *rds.ModifyDBClusterInput, opts ...request.Option) (*rds.ModifyDBClusterOutput, error)
	DescribeDBClustersWithContext(ctx aws.Context, input *rds.DescribeDBClustersInput, opts ...request.Option) (*rds.DescribeDBClustersOutput, error)
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
