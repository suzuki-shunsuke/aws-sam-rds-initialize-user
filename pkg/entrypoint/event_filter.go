package entrypoint

import (
	"context"
	"errors"
	"fmt"

	"github.com/d5/tengo/v2"
	"github.com/d5/tengo/v2/stdlib"
	"github.com/suzuki-shunsuke/aws-sam-rds-initialize-user/pkg/constant"
)

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
