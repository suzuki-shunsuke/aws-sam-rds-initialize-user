# aws-sam-rds-initialize-user

AWS SAM application to initialize RDS DB users

## Status

Still work in progress.

## Motivation

To reset RDS master user's password and create a Database user for application automatically when a RDS database is created.
The random passwords of master user and application user are generated.
The password of master user isn't persisted anywhere.
If you need to connect to the database as a master user, you have to change the master password by [aws rds modify-rds-cluster](https://docs.aws.amazon.com/cli/latest/reference/rds/modify-db-cluster.html).
The application user information is stored as AWS Secrets Manager's secret. 

## LICENSE

[MIT](LICENSE)
