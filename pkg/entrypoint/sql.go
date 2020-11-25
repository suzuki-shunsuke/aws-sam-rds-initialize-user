package entrypoint

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/d5/tengo/v2"
	"github.com/d5/tengo/v2/stdlib"
	"github.com/sirupsen/logrus"
	"github.com/suzuki-shunsuke/aws-sam-rds-initialize-user/pkg/constant"
)

type Transaction interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	Commit() error
	Rollback() error
}

func (ep Entrypoint) runSQLsTx(ctx context.Context, tx Transaction, queries []Query) error {
	for _, query := range queries {
		if _, err := tx.ExecContext(ctx, query.Query, query.Args...); err != nil {
			return fmt.Errorf("execute a SQL: %w", err)
		}
	}
	return nil
}

func (ep Entrypoint) runSQLs(ctx context.Context, connInfo DBConnectInfo, queries []Query) error {
	db, err := sql.Open(connInfo.Driver, connInfo.DSN())
	if err != nil {
		return fmt.Errorf("connect to the database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logrus.WithError(err).Error("close a database connection")
		}
	}()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin a database transaction: %w", err)
	}
	if err := ep.runSQLsTx(ctx, tx, queries); err != nil {
		if e := tx.Rollback(); err != nil {
			return fmt.Errorf("execute a SQL: %w: rollback: %s", err, e.Error()) //nolint:errorlint
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit a database transaction: %w", err)
	}
	return nil
}

type Query struct {
	Query string
	Args  []interface{}
}

func (ep Entrypoint) evaluate(ctx context.Context, scr string, dbCluster interface{}, passwords map[string]string) (*tengo.Compiled, error) {
	script := tengo.NewScript([]byte(scr))
	script.SetImports(stdlib.GetModuleMap(stdlib.AllModuleNames()...))
	if err := script.Add("DBCluster", dbCluster); err != nil {
		return nil, fmt.Errorf("add DBCluster to the Tengo script: %w", err)
	}
	if err := script.Add("Passwords", passwords); err != nil {
		return nil, fmt.Errorf("add Passwords to the Tengo script: %w", err)
	}
	compiled, err := script.RunContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("compile and run the Tengo script: %w", err)
	}
	if !compiled.IsDefined(constant.Result) {
		return nil, constant.ErrNoBoolVariable
	}
	return compiled, nil
}

func (ep Entrypoint) evaluateSQL(
	ctx context.Context, dbCluster interface{}, cfg Config, passwords map[string]string,
) ([]Query, error) {
	compiled, err := ep.evaluate(ctx, cfg.SQL, dbCluster, passwords)
	if err != nil {
		return nil, err
	}
	v := compiled.Get(constant.Result)
	if t := v.ValueType(); t != "array" {
		return nil, errors.New(`the type of the variable "result" should be array, but actually ` + t)
	}
	arr := v.Array()
	return ep.parseEvaluatedSQL(arr)
}

func (ep Entrypoint) parseEvaluatedSQL(arr []interface{}) ([]Query, error) {
	queries := make([]Query, len(arr))
	for i, a := range arr {
		switch b := a.(type) {
		case string:
			queries[i] = Query{
				Query: b,
			}
			continue
		case map[string]interface{}:
			q, ok := b["query"]
			if !ok {
				return nil, errors.New(`the field "query" isn't found`)
			}
			qs, ok := q.(string)
			if !ok {
				return nil, fmt.Errorf(`query should be string: %+v`, q)
			}
			c, ok := b["args"]
			if !ok {
				queries[i] = Query{
					Query: qs,
				}
				continue
			}
			d, ok := c.([]interface{})
			if !ok {
				return nil, fmt.Errorf(`args should be array: %+v`, c)
			}
			queries[i] = Query{
				Query: qs,
				Args:  d,
			}
			continue
		default:
			return nil, fmt.Errorf(`query should be string or map: %+v`, a)
		}
	}
	return queries, nil
}
