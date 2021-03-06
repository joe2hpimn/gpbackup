package utils_test

import (
	"os"
	"time"

	"github.com/greenplum-db/gpbackup/testutils"
	"github.com/greenplum-db/gpbackup/utils"

	"github.com/jmoiron/sqlx"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"gopkg.in/DATA-DOG/go-sqlmock.v1"
)

var connection *utils.DBConn
var mock sqlmock.Sqlmock

var _ = Describe("utils/db tests", func() {
	BeforeEach(func() {
		testutils.SetupTestLogger()
		utils.System.Now = func() time.Time { return time.Date(2017, time.January, 1, 1, 1, 1, 1, time.Local) }
	})
	Describe("NewDBConn", func() {
		Context("Database given with -dbname flag", func() {
			It("gets the DBName from dbname argument", func() {
				connection = utils.NewDBConn("testdb")
				Expect(connection.DBName).To(Equal("testdb"))
			})
		})
		Context("No database given with -dbname flag but PGDATABASE set", func() {
			It("gets the DBName from PGDATABASE", func() {
				oldPgDatabase := os.Getenv("PGDATABASE")
				os.Setenv("PGDATABASE", "testdb")
				defer os.Setenv("PGDATABASE", oldPgDatabase)

				connection = utils.NewDBConn("")
				Expect(connection.DBName).To(Equal("testdb"))
			})
		})
		Context("No database given with either -dbname or PGDATABASE", func() {
			It("fails", func() {
				oldPgDatabase := os.Getenv("PGDATABASE")
				os.Setenv("PGDATABASE", "")
				defer os.Setenv("PGDATABASE", oldPgDatabase)

				defer testutils.ShouldPanicWithMessage("No database provided and PGDATABASE not set")
				connection = utils.NewDBConn("")
			})
		})
	})
	Describe("DBConn.Connect", func() {
		Context("The database exists", func() {
			It("connects successfully", func() {
				var mockdb *sqlx.DB
				mockdb, mock = testutils.CreateMockDB()
				driver := testutils.TestDriver{DBExists: true, RoleExists: true, DB: mockdb, User: "testrole"}
				connection = utils.NewDBConn("testdb")
				connection.Driver = driver
				Expect(connection.DBName).To(Equal("testdb"))
				connection.Connect()
			})
		})
		Context("The database does not exist", func() {
			It("fails", func() {
				var mockdb *sqlx.DB
				mockdb, mock = testutils.CreateMockDB()
				driver := testutils.TestDriver{DBExists: false, RoleExists: true, DB: mockdb, DBName: "testdb", User: "testrole"}
				connection = utils.NewDBConn("testdb")
				connection.Driver = driver
				Expect(connection.DBName).To(Equal("testdb"))
				defer testutils.ShouldPanicWithMessage("Database \"testdb\" does not exist, exiting")
				connection.Connect()
			})
		})
		Context("The role does not exist", func() {
			It("fails", func() {
				var mockdb *sqlx.DB
				mockdb, mock = testutils.CreateMockDB()
				driver := testutils.TestDriver{DBExists: true, RoleExists: false, DB: mockdb, DBName: "testdb", User: "nonexistent"}

				oldPgUser := os.Getenv("PGUSER")
				os.Setenv("PGUSER", "nonexistent")
				defer os.Setenv("PGUSER", oldPgUser)

				connection = utils.NewDBConn("testdb")
				connection.Driver = driver
				Expect(connection.User).To(Equal("nonexistent"))
				defer testutils.ShouldPanicWithMessage("Role \"nonexistent\" does not exist, exiting")
				connection.Connect()
			})
		})
	})
	Describe("DBConn.Exec", func() {
		It("executes an INSERT outside of a transaction", func() {
			connection, mock = testutils.CreateAndConnectMockDB()
			fakeResult := testutils.TestResult{Rows: 1}
			mock.ExpectExec("INSERT (.*)").WillReturnResult(fakeResult)

			res, err := connection.Exec("INSERT INTO pg_tables VALUES ('schema', 'table')")
			Expect(err).ToNot(HaveOccurred())
			rowsReturned, err := res.RowsAffected()
			Expect(rowsReturned).To(Equal(int64(1)))
		})
		It("executes an INSERT in a transaction", func() {
			connection, mock = testutils.CreateAndConnectMockDB()
			fakeResult := testutils.TestResult{Rows: 1}
			testutils.ExpectBegin(mock)
			mock.ExpectExec("INSERT (.*)").WillReturnResult(fakeResult)
			mock.ExpectCommit()

			connection.Begin()
			res, err := connection.Exec("INSERT INTO pg_tables VALUES ('schema', 'table')")
			connection.Commit()
			Expect(err).ToNot(HaveOccurred())
			rowsReturned, err := res.RowsAffected()
			Expect(rowsReturned).To(Equal(int64(1)))
		})
	})
	Describe("DBConn.Get", func() {
		It("executes a GET outside of a transaction", func() {
			connection, mock = testutils.CreateAndConnectMockDB()
			two_col_single_row := sqlmock.NewRows([]string{"schemaname", "tablename"}).
				AddRow("schema1", "table1")
			mock.ExpectQuery("SELECT (.*)").WillReturnRows(two_col_single_row)

			testRecord := struct {
				Schemaname string
				Tablename  string
			}{}

			err := connection.Get(&testRecord, "SELECT schemaname, tablename FROM two_columns ORDER BY schemaname")

			Expect(err).ToNot(HaveOccurred())
			Expect(testRecord.Schemaname).To(Equal("schema1"))
			Expect(testRecord.Tablename).To(Equal("table1"))
		})
		It("executes a GET in a transaction", func() {
			connection, mock = testutils.CreateAndConnectMockDB()
			two_col_single_row := sqlmock.NewRows([]string{"schemaname", "tablename"}).
				AddRow("schema1", "table1")
			testutils.ExpectBegin(mock)
			mock.ExpectQuery("SELECT (.*)").WillReturnRows(two_col_single_row)
			mock.ExpectCommit()

			testRecord := struct {
				Schemaname string
				Tablename  string
			}{}

			connection.Begin()
			err := connection.Get(&testRecord, "SELECT schemaname, tablename FROM two_columns ORDER BY schemaname")
			connection.Commit()
			Expect(err).ToNot(HaveOccurred())
			Expect(testRecord.Schemaname).To(Equal("schema1"))
			Expect(testRecord.Tablename).To(Equal("table1"))
		})
	})
	Describe("DBConn.Select", func() {
		It("executes a SELECT outside of a transaction", func() {
			connection, mock = testutils.CreateAndConnectMockDB()
			two_col_rows := sqlmock.NewRows([]string{"schemaname", "tablename"}).
				AddRow("schema1", "table1").
				AddRow("schema2", "table2")
			mock.ExpectQuery("SELECT (.*)").WillReturnRows(two_col_rows)

			testSlice := make([]struct {
				Schemaname string
				Tablename  string
			}, 0)

			err := connection.Select(&testSlice, "SELECT schemaname, tablename FROM two_columns ORDER BY schemaname LIMIT 2")

			Expect(err).ToNot(HaveOccurred())
			Expect(len(testSlice)).To(Equal(2))
			Expect(testSlice[0].Schemaname).To(Equal("schema1"))
			Expect(testSlice[0].Tablename).To(Equal("table1"))
			Expect(testSlice[1].Schemaname).To(Equal("schema2"))
			Expect(testSlice[1].Tablename).To(Equal("table2"))
		})
		It("executes a SELECT in a transaction", func() {
			connection, mock = testutils.CreateAndConnectMockDB()
			two_col_rows := sqlmock.NewRows([]string{"schemaname", "tablename"}).
				AddRow("schema1", "table1").
				AddRow("schema2", "table2")
			testutils.ExpectBegin(mock)
			mock.ExpectQuery("SELECT (.*)").WillReturnRows(two_col_rows)
			mock.ExpectCommit()

			testSlice := make([]struct {
				Schemaname string
				Tablename  string
			}, 0)

			connection.Begin()
			err := connection.Select(&testSlice, "SELECT schemaname, tablename FROM two_columns ORDER BY schemaname LIMIT 2")
			connection.Commit()

			Expect(err).ToNot(HaveOccurred())
			Expect(len(testSlice)).To(Equal(2))
			Expect(testSlice[0].Schemaname).To(Equal("schema1"))
			Expect(testSlice[0].Tablename).To(Equal("table1"))
			Expect(testSlice[1].Schemaname).To(Equal("schema2"))
			Expect(testSlice[1].Tablename).To(Equal("table2"))
		})
	})
})
