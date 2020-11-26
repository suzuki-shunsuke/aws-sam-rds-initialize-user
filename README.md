# aws-sam-rds-initialize-user

[![Build Status](https://github.com/suzuki-shunsuke/aws-sam-rds-initialize-user/workflows/CI/badge.svg)](https://github.com/suzuki-shunsuke/aws-sam-rds-initialize-user/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/suzuki-shunsuke/aws-sam-rds-initialize-user)](https://goreportcard.com/report/github.com/suzuki-shunsuke/aws-sam-rds-initialize-user)
[![GitHub last commit](https://img.shields.io/github/last-commit/suzuki-shunsuke/aws-sam-rds-initialize-user.svg)](https://github.com/suzuki-shunsuke/aws-sam-rds-initialize-user)
[![License](http://img.shields.io/badge/license-mit-blue.svg?style=flat-square)](https://raw.githubusercontent.com/suzuki-shunsuke/aws-sam-rds-initialize-user/master/LICENSE)

AWS SAM application to reset RDS master user's password and create a Database user for application automatically when a RDS database is created.

## Status

Still work in progress.

## Motivation

The motivation is to automate the dabase setup (ex. create user) securely.
This application generates a database user for application according to the best practice.

https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/UsingWithRDS.MasterAccounts.html

> We strongly recommend that you do not use the master user directly in your applications.
> Instead, adhere to the best practice of using a database user created with the minimal privileges required for your application.

The database user's password is generated dynamically and stored as AWS Secrets Manager's secret,
so anyone doesn't have to know the password itself. It is desirable in terms of security.

The password of master user is reset and isn't persisted anywhere, so anyone can't know the password.
If you need to connect to the database as a master user, you have to change the master password by [aws rds modify-rds-cluster](https://docs.aws.amazon.com/cli/latest/reference/rds/modify-db-cluster.html).

## How to work

https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/rds-cloud-watch-events.html

When a RDS cluster is created, this Lambda function is triggered by EventBridge.
the Lambda Function changes the master user's password to a random string, and connect the database and execute SQL.
To connect the RDS cluster, this Lambda function should be connected to VPC.

## How to setup

Coming soon.

## Configuration

### Environment variables

* EVENT_FILTER: Tengo script which returns `true` if the event is processed. If the script returns `false`, the event is ignored
* USERS: a list of user names which are joined by a space `" "`
  Passwords are generated and the map of user name and password is passed to Tengo script.
* SQL: Tengo script which returns a list of executed SQLs
* SECRETS: Tengo script which returns a list of secrets

### Tengo

https://github.com/d5/tengo

To support flexible configuration, this Lambda function adopts [Tengo](https://github.com/d5/tengo) in the configuration.

### Parameters of Tengo Script

* DBCluster: The response of rds describe clusters API
  * https://awscli.amazonaws.com/v2/documentation/api/latest/reference/rds/describe-db-clusters.html#output
* Passwords: `map[string]string`. The key is a user name and the value is a generated password.

### Example

Please see [examples/template.yaml](examples/template.yaml).

## LICENSE

[MIT](LICENSE)
