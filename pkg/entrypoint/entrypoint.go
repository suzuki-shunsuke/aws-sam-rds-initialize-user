package entrypoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/sethvargo/go-password/password"
	"github.com/sirupsen/logrus"
)

type Entrypoint struct{}

func (ep Entrypoint) Start(ctx context.Context, ev events.CloudWatchEvent) error {
	logrus.SetFormatter(&logrus.JSONFormatter{})
	if err := ep.start(ctx, ev); err != nil {
		logrus.WithError(err).Error("start")
		return err
	}
	return nil
}

type EventDetail struct {
	SourceIdentifier string
}

func (ep Entrypoint) start(ctx context.Context, ev events.CloudWatchEvent) error {
	// create a user
	// create AWS Secrets Manager's secret for user password
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
	if err := ep.changeMasterPassword(ctx, rdsSvc, ed.SourceIdentifier); err != nil {
		return fmt.Errorf("change the master user password: %w", err)
	}
	logE.Info("master user password is changed to a random password")
	return nil
}

type RDSService interface {
	ModifyDBClusterWithContext(ctx aws.Context, input *rds.ModifyDBClusterInput, opts ...request.Option) (*rds.ModifyDBClusterOutput, error)
}

func (ep Entrypoint) changeMasterPassword(ctx context.Context, rdsSvc RDSService, identifier string) error {
	pw, err := password.Generate(32, 10, 0, true, true)
	if err != nil {
		return fmt.Errorf("generate a master user password: %w", err)
	}

	_, err = rdsSvc.ModifyDBClusterWithContext(ctx, &rds.ModifyDBClusterInput{
		ApplyImmediately:    aws.Bool(true),
		DBClusterIdentifier: aws.String(identifier),
		MasterUserPassword:  aws.String(pw),
	})
	return err
}
