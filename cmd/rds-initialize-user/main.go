package main

import (
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/suzuki-shunsuke/aws-sam-rds-initialize-user/pkg/entrypoint"
)

func main() {
	ep := entrypoint.Entrypoint{}
	lambda.Start(ep.Start)
}
