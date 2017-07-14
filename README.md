# Greenplum Backup

`gpbackup` and `gprestore` are Golang utilities for performing backups and restores of a Greenplum Database. They are still currently in active development.

## Pre-Requisites

You will need Go version 1.8 or higher to work with our code.
Follow the directions [here](https://golang.org/doc/) to get the language set up.

## Installing

`go get github.com/greenplum-db/gpbackup`

This will place the code in `$GOPATH/github.com/greenplum-db/gpbackup`

## Building binaries

cd into the gpbackup directory and run

```bash
make depend
make build
```

This will put the gpbackup and gprestore binaries in `$HOME/go/bin`

`make build_rhel` and `make build_osx` are for cross compiling between osx and redhat

## Running the utilities

The basic command for gpbackup is
```bash
gpbackup --dbname <your_db_name>
```

The basic command for gprestore is
```bash
gprestore --timestamp <YYYYMMDDHHMMSS>
```

Run `--help` with either command for a complete list of options

## Running tests

To run all tests, use
```bash
make test
```

To run only unit tests, use
```bash
make unit
```

To run only integration tests (which require a running GPDB instance), use
```bash
make integration
```

## Cleaning up

To remove the compiled binaries and other generated files, run
```bash
make clean
```

## Project structure

This repository has several different packages:

### backup
Functions that are directly responsible for performing backup operations.

The files inside backup are divided according to what backup file an object will go into during the backup (predata, postdata, data, global) and what type of object it is (regtable: regular tables, exttable: external tables, and nontable: all other objects).

There is also a backup_test package under the backup directory for unit tests that pertain to backup code.

### restore
Functions that are directly responsible for performing restore operations.

### integration
Integration test files.

### utils
Functions and structs that are used by both the backup and restore packages. This includes operations such as database access, input parsing, and logging.

### testutils
Functions and structs that are used for testing.

# How to Contribute

We accept contributions via [Github Pull requests](https://help.github.com/articles/using-pull-requests) only.

Follow the steps below to open a PR:
1. Fork the project’s repository
1. Create your own feature branch (e.g. `git checkout -b gpbackup_branch`) and make changes on this branch.
    * Follow the previous sections on this page to setup and build in your environment.
	* Make sure you still `go get github/com/greenplum-db/gpbackup` and not your own fork. Otherwise you will encounter errors with import paths.
1. Run through all the tests in your feature branch and ensure they are successful.
    * Add new tests to cover your code. We use Ginkgo and Gomega for testing. Make your best guess as to which file the code and tests should go in by following the names and comments. We can provide feedback in the PR if anything should be moved to a different file.
1. Add your fork as a remote to the repository and push your local branch to the fork (e.g. `git push <your_fork> gpbackup_branch`) and [submit a pull request](https://help.github.com/articles/creating-a-pull-request)
    * Be sure to run `make format` before you submit your pull request.

Your contribution will be analyzed for product fit and engineering quality prior to merging.
Note: All contributions must be sent using GitHub Pull Requests.

**Your pull request is much more likely to be accepted if it is small and focused with a clear message that conveys the intent of your change.**

Overall we follow GPDB's comprehensive contribution policy. Please refer to it [here](https://github.com/greenplum-db/gpdb#contributing) for details.

