package entrypoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/suzuki-shunsuke/aws-sam-rds-initialize-user/pkg/constant"
)

type Secret struct {
	Name   string
	Secret string
}

func (ep Entrypoint) evaluateSecrets(
	ctx context.Context, dbCluster interface{}, cfg Config, passwords map[string]string,
) ([]Secret, error) {
	compiled, err := ep.evaluate(ctx, cfg.SQL, dbCluster, passwords)
	if err != nil {
		return nil, err
	}
	v := compiled.Get(constant.Result)
	if t := v.ValueType(); t != "array" {
		return nil, errors.New(`the type of the variable "result" should be array, but actually ` + t)
	}
	arr := v.Array()
	return ep.parseSecrets(arr)
}

func (ep Entrypoint) parseSecrets(arr []interface{}) ([]Secret, error) {
	secrets := make([]Secret, len(arr))
	for i, a := range arr {
		switch b := a.(type) {
		case map[string]interface{}:
			q, ok := b["name"]
			if !ok {
				return nil, errors.New(`the field "name" isn't found`)
			}
			qs, ok := q.(string)
			if !ok {
				return nil, fmt.Errorf(`name should be string: %+v`, q)
			}
			c, ok := b["secret"]
			if !ok {
				return nil, errors.New(`the field "secret" isn't found`)
			}
			switch d := c.(type) {
			case string:
				secrets[i] = Secret{
					Name:   qs,
					Secret: d,
				}
				continue
			default:
				e, err := json.Marshal(c)
				if err != nil {
					return nil, fmt.Errorf(`marshal secret as JSON: %w`, err)
				}
				secrets[i] = Secret{
					Name:   qs,
					Secret: string(e),
				}
				continue
			}
		default:
			return nil, fmt.Errorf(`query should be string or map: %+v`, a)
		}
	}
	return secrets, nil
}
