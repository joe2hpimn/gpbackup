package integration

import (
	"github.com/greenplum-db/gpbackup/backup"
	"github.com/greenplum-db/gpbackup/testutils"
	"github.com/greenplum-db/gpbackup/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("backup integration tests", func() {
	BeforeEach(func() {
		testutils.SetupTestLogger()
	})
	Describe("GetAllUserSchemas", func() {
		It("returns user schema information", func() {
			testutils.AssertQueryRuns(connection, "CREATE SCHEMA bar")
			defer testutils.AssertQueryRuns(connection, "DROP SCHEMA bar")
			schemas := backup.GetAllUserSchemas(connection)

			schemaBar := utils.Schema{0, "bar", "", "testrole"}
			schemaPublic := utils.Schema{2200, "public", "standard public schema", "testrole"}

			Expect(len(schemas)).To(Equal(2))
			testutils.ExpectStructsToMatchExcluding(&schemaBar, &schemas[0], "SchemaOid")
			testutils.ExpectStructsToMatchExcluding(&schemaPublic, &schemas[1], "Owner")
		})
	})
	Describe("GetAllUserTables", func() {
		It("returns user table information for basic heap tables", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE foo(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE foo")
			testutils.AssertQueryRuns(connection, "CREATE SCHEMA testschema")
			defer testutils.AssertQueryRuns(connection, "DROP SCHEMA testschema CASCADE")
			testutils.AssertQueryRuns(connection, "CREATE TABLE testschema.testtable(t text)")

			tables := backup.GetAllUserTables(connection)

			tableFoo := utils.BasicRelation("public", "foo")
			tableTestTable := utils.BasicRelation("testschema", "testtable")

			Expect(len(tables)).To(Equal(2))
			testutils.ExpectStructsToMatchExcluding(&tableFoo, &tables[0], "SchemaOid", "RelationOid")
			testutils.ExpectStructsToMatchExcluding(&tableTestTable, &tables[1], "SchemaOid", "RelationOid")
		})
		It("only returns the parent partition table for partition tables", func() {
			createStmt := `CREATE TABLE rank (id int, rank int, year int, gender
char(1), count int )
DISTRIBUTED BY (id)
PARTITION BY LIST (gender)
( PARTITION girls VALUES ('F'),
  PARTITION boys VALUES ('M'),
  DEFAULT PARTITION other );`
			testutils.AssertQueryRuns(connection, createStmt)
			defer testutils.AssertQueryRuns(connection, "DROP TABLE rank")

			tables := backup.GetAllUserTables(connection)

			tableRank := utils.BasicRelation("public", "rank")

			Expect(len(tables)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&tableRank, &tables[0], "SchemaOid", "RelationOid")
		})
	})
	Describe("GetTableAttributes", func() {
		It("returns table attribute information for a heap table", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE atttable(a float, b text, c text NOT NULL, d int DEFAULT(5))")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE atttable")
			testutils.AssertQueryRuns(connection, "COMMENT ON COLUMN atttable.a IS 'att comment'")
			testutils.AssertQueryRuns(connection, "ALTER TABLE atttable DROP COLUMN b")
			oid := testutils.OidFromRelationName(connection, "atttable")

			tableAtts := backup.GetTableAttributes(connection, oid)

			columnA := backup.QueryTableAtts{1, "a", false, false, false, "double precision", "", "att comment"}
			columnC := backup.QueryTableAtts{3, "c", true, false, false, "text", "", ""}
			columnD := backup.QueryTableAtts{4, "d", false, true, false, "integer", "", ""}

			Expect(len(tableAtts)).To(Equal(3))

			testutils.ExpectStructsToMatch(&columnA, &tableAtts[0])
			testutils.ExpectStructsToMatch(&columnC, &tableAtts[1])
			testutils.ExpectStructsToMatch(&columnD, &tableAtts[2])
		})
		It("returns table attributes including encoding for a column oriented table", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE co_atttable(a float, b text ENCODING(blocksize=65536)) WITH (appendonly=true, orientation=column)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE co_atttable")
			oid := testutils.OidFromRelationName(connection, "co_atttable")

			tableAtts := backup.GetTableAttributes(connection, uint32(oid))

			columnA := backup.QueryTableAtts{1, "a", false, false, false, "double precision", "compresstype=none,blocksize=32768,compresslevel=0", ""}
			columnB := backup.QueryTableAtts{2, "b", false, false, false, "text", "blocksize=65536,compresstype=none,compresslevel=0", ""}

			Expect(len(tableAtts)).To(Equal(2))

			testutils.ExpectStructsToMatch(&columnA, &tableAtts[0])
			testutils.ExpectStructsToMatch(&columnB, &tableAtts[1])
		})
		It("returns an empty attribute array for a table with no columns", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE nocol_atttable()")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE nocol_atttable")
			oid := testutils.OidFromRelationName(connection, "nocol_atttable")

			tableAtts := backup.GetTableAttributes(connection, uint32(oid))

			Expect(len(tableAtts)).To(Equal(0))
		})
	})
	Describe("GetTableDefaults", func() {
		It("only returns defaults for columns that have them", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE default_table(a text DEFAULT('default text'), b float, c int DEFAULT(5))")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE default_table")
			oid := testutils.OidFromRelationName(connection, "default_table")

			defaults := backup.GetTableDefaults(connection, oid)

			Expect(len(defaults)).To(Equal(2))

			Expect(defaults[0].AdNum).To(Equal(1))
			Expect(defaults[0].DefaultVal).To(Equal("'default text'::text"))

			Expect(defaults[1].AdNum).To(Equal(3))
			Expect(defaults[1].DefaultVal).To(Equal("5"))
		})
		It("returns an empty default array for a table with no defaults", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE nodefault_table(a text, b float, c int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE nodefault_table")
			oid := testutils.OidFromRelationName(connection, "nodefault_table")

			defaults := backup.GetTableDefaults(connection, oid)

			Expect(len(defaults)).To(Equal(0))
		})
	})
	Describe("GetConstraints", func() {
		var (
			uniqueConstraint = backup.QueryConstraint{"uniq2", "u", "UNIQUE (a, b)", "this is a constraint comment"}
			fkConstraint     = backup.QueryConstraint{"fk1", "f", "FOREIGN KEY (b) REFERENCES constraints_other_table(b)", ""}
			pkConstraint     = backup.QueryConstraint{"pk1", "p", "PRIMARY KEY (a, b)", "this is a constraint comment"}
			checkConstraint  = backup.QueryConstraint{"check1", "c", "CHECK (a <> 42)", ""}
		)
		Context("No constraints", func() {
			It("returns an empty constraint array for a table with no constraints", func() {
				testutils.AssertQueryRuns(connection, "CREATE TABLE no_constraints_table(a int, b text)")
				defer testutils.AssertQueryRuns(connection, "DROP TABLE no_constraints_table")
				oid := testutils.OidFromRelationName(connection, "no_constraints_table")

				constraints := backup.GetConstraints(connection, oid)

				Expect(len(constraints)).To(Equal(0))
			})
		})
		Context("One constraint", func() {
			It("returns a constraint array for a table with one UNIQUE constraint and a comment", func() {
				testutils.AssertQueryRuns(connection, "CREATE TABLE constraints_table(a int, b text, c float)")
				defer testutils.AssertQueryRuns(connection, "DROP TABLE constraints_table")
				testutils.AssertQueryRuns(connection, "ALTER TABLE ONLY constraints_table ADD CONSTRAINT uniq2 UNIQUE (a, b)")
				testutils.AssertQueryRuns(connection, "COMMENT ON CONSTRAINT uniq2 ON constraints_table IS 'this is a constraint comment'")
				oid := testutils.OidFromRelationName(connection, "constraints_table")

				constraints := backup.GetConstraints(connection, oid)

				Expect(len(constraints)).To(Equal(1))
				Expect(constraints[0]).To(Equal(uniqueConstraint))
			})
			It("returns a constraint array for a table with one PRIMARY KEY constraint and a comment", func() {
				testutils.AssertQueryRuns(connection, "CREATE TABLE constraints_table(a int, b text, c float)")
				defer testutils.AssertQueryRuns(connection, "DROP TABLE constraints_table")
				testutils.AssertQueryRuns(connection, "ALTER TABLE ONLY constraints_table ADD CONSTRAINT pk1 PRIMARY KEY (a, b)")
				testutils.AssertQueryRuns(connection, "COMMENT ON CONSTRAINT pk1 ON constraints_table IS 'this is a constraint comment'")
				oid := testutils.OidFromRelationName(connection, "constraints_table")

				constraints := backup.GetConstraints(connection, oid)

				Expect(len(constraints)).To(Equal(1))
				Expect(constraints[0]).To(Equal(pkConstraint))
			})
			It("returns a constraint array for a table with one FOREIGN KEY constraint", func() {
				testutils.AssertQueryRuns(connection, "CREATE TABLE constraints_table(a int, b text, c float)")
				defer testutils.AssertQueryRuns(connection, "DROP TABLE constraints_table CASCADE")
				testutils.AssertQueryRuns(connection, "CREATE TABLE constraints_other_table(b text)")
				defer testutils.AssertQueryRuns(connection, "DROP TABLE constraints_other_table CASCADE")
				testutils.AssertQueryRuns(connection, "ALTER TABLE ONLY constraints_other_table ADD CONSTRAINT uniq1 UNIQUE (b)")
				testutils.AssertQueryRuns(connection, "ALTER TABLE ONLY constraints_table ADD CONSTRAINT fk1 FOREIGN KEY (b) REFERENCES constraints_other_table(b)")
				oid := testutils.OidFromRelationName(connection, "constraints_table")

				constraints := backup.GetConstraints(connection, oid)

				Expect(len(constraints)).To(Equal(1))
				Expect(constraints[0]).To(Equal(fkConstraint))
			})
			It("returns a constraint array for a table with one CHECK constraint", func() {
				testutils.AssertQueryRuns(connection, "CREATE TABLE constraints_table(a int, b text, c float)")
				defer testutils.AssertQueryRuns(connection, "DROP TABLE constraints_table")
				testutils.AssertQueryRuns(connection, "ALTER TABLE ONLY constraints_table ADD CONSTRAINT check1 CHECK (a <> 42)")
				oid := testutils.OidFromRelationName(connection, "constraints_table")

				constraints := backup.GetConstraints(connection, oid)

				Expect(len(constraints)).To(Equal(1))
				Expect(constraints[0]).To(Equal(checkConstraint))
			})
		})
		Context("Multiple constraints", func() {
			It("returns a constraint array for a table with multiple constraints", func() {
				testutils.AssertQueryRuns(connection, "CREATE TABLE constraints_table(a int, b text, c float)")
				defer testutils.AssertQueryRuns(connection, "DROP TABLE constraints_table CASCADE")
				testutils.AssertQueryRuns(connection, "CREATE TABLE constraints_other_table(b text)")
				defer testutils.AssertQueryRuns(connection, "DROP TABLE constraints_other_table CASCADE")
				testutils.AssertQueryRuns(connection, "ALTER TABLE ONLY constraints_other_table ADD CONSTRAINT uniq1 UNIQUE (b)")
				testutils.AssertQueryRuns(connection, "ALTER TABLE ONLY constraints_table ADD CONSTRAINT uniq2 UNIQUE (a, b)")
				testutils.AssertQueryRuns(connection, "COMMENT ON CONSTRAINT uniq2 ON constraints_table IS 'this is a constraint comment'")
				testutils.AssertQueryRuns(connection, "ALTER TABLE ONLY constraints_table ADD CONSTRAINT pk1 PRIMARY KEY (a, b)")
				testutils.AssertQueryRuns(connection, "COMMENT ON CONSTRAINT pk1 ON constraints_table IS 'this is a constraint comment'")
				testutils.AssertQueryRuns(connection, "ALTER TABLE ONLY constraints_table ADD CONSTRAINT fk1 FOREIGN KEY (b) REFERENCES constraints_other_table(b)")
				testutils.AssertQueryRuns(connection, "ALTER TABLE ONLY constraints_table ADD CONSTRAINT check1 CHECK (a <> 42)")
				oid := testutils.OidFromRelationName(connection, "constraints_table")

				constraints := backup.GetConstraints(connection, oid)

				Expect(len(constraints)).To(Equal(4))
				Expect(constraints[0]).To(Equal(uniqueConstraint))
				Expect(constraints[1]).To(Equal(pkConstraint))
				Expect(constraints[2]).To(Equal(fkConstraint))
				Expect(constraints[3]).To(Equal(checkConstraint))
			})
		})
	})
	Describe("GetDistributionPolicy", func() {
		It("returns distribution policy info for a table DISTRIBUTED RANDOMLY", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE dist_random(a int, b text) DISTRIBUTED RANDOMLY")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE dist_random")
			oid := testutils.OidFromRelationName(connection, "dist_random")

			distPolicy := backup.GetDistributionPolicy(connection, oid)

			Expect(distPolicy).To(Equal("DISTRIBUTED RANDOMLY"))
		})
		It("returns distribution policy info for a table DISTRIBUTED BY one column", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE dist_one(a int, b text) DISTRIBUTED BY (a)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE dist_one")
			oid := testutils.OidFromRelationName(connection, "dist_one")

			distPolicy := backup.GetDistributionPolicy(connection, oid)

			Expect(distPolicy).To(Equal("DISTRIBUTED BY (a)"))
		})
		It("returns distribution policy info for a table DISTRIBUTED BY two columns", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE dist_two(a int, b text) DISTRIBUTED BY (a, b)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE dist_two")
			oid := testutils.OidFromRelationName(connection, "dist_two")

			distPolicy := backup.GetDistributionPolicy(connection, oid)

			Expect(distPolicy).To(Equal("DISTRIBUTED BY (a, b)"))
		})
	})
	Describe("GetAllSequenceRelations", func() {
		It("", func() {
			testutils.AssertQueryRuns(connection, "CREATE SEQUENCE my_sequence START 10")
			defer testutils.AssertQueryRuns(connection, "DROP SEQUENCE my_sequence")
			testutils.AssertQueryRuns(connection, "COMMENT ON SEQUENCE public.my_sequence IS 'this is a sequence comment'")

			testutils.AssertQueryRuns(connection, "CREATE SCHEMA testschema")
			defer testutils.AssertQueryRuns(connection, "DROP SCHEMA testschema CASCADE")
			testutils.AssertQueryRuns(connection, "CREATE SEQUENCE testschema.my_sequence2")

			sequences := backup.GetAllSequenceRelations(connection)

			mySequence := utils.Relation{0, 0, "public", "my_sequence", "this is a sequence comment", "testrole"}
			mySequence2 := utils.Relation{0, 0, "testschema", "my_sequence2", "", "testrole"}

			Expect(len(sequences)).To(Equal(2))
			testutils.ExpectStructsToMatchExcluding(&mySequence, &sequences[0], "SchemaOid", "RelationOid")
			testutils.ExpectStructsToMatchExcluding(&mySequence2, &sequences[1], "SchemaOid", "RelationOid")
		})
	})
	Describe("GetSequenceDefinition", func() {
		It("returns sequence information for sequence with default values", func() {
			testutils.AssertQueryRuns(connection, "CREATE SEQUENCE my_sequence")
			defer testutils.AssertQueryRuns(connection, "DROP SEQUENCE my_sequence")

			resultSequenceDef := backup.GetSequenceDefinition(connection, "my_sequence")

			expectedSequence := backup.QuerySequenceDefinition{Name: "my_sequence", LastVal: 1, Increment: 1, MaxVal: 9223372036854775807, MinVal: 1, CacheVal: 1}

			testutils.ExpectStructsToMatch(&expectedSequence, &resultSequenceDef)
		})
		It("returns sequence information for a complex sequence", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE with_sequence(a int, b char(20))")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE with_sequence")
			testutils.AssertQueryRuns(connection,
				"CREATE SEQUENCE my_sequence INCREMENT BY 5 MINVALUE 20 MAXVALUE 1000 START 100 OWNED BY with_sequence.a")
			defer testutils.AssertQueryRuns(connection, "DROP SEQUENCE my_sequence")
			testutils.AssertQueryRuns(connection, "INSERT INTO with_sequence VALUES (nextval('my_sequence'), 'acme')")
			testutils.AssertQueryRuns(connection, "INSERT INTO with_sequence VALUES (nextval('my_sequence'), 'beta')")

			resultSequenceDef := backup.GetSequenceDefinition(connection, "my_sequence")

			expectedSequence := backup.QuerySequenceDefinition{Name: "my_sequence", LastVal: 105, Increment: 5, MaxVal: 1000, MinVal: 20, CacheVal: 1, LogCnt: 31, IsCycled: false, IsCalled: true}

			testutils.ExpectStructsToMatch(&expectedSequence, &resultSequenceDef)
		})
	})
	Describe("GetSequenceOwnerMap", func() {
		It("returns sequence information for sequences owned by columns", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE without_sequence(a int, b char(20));")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE without_sequence")
			testutils.AssertQueryRuns(connection, "CREATE TABLE with_sequence(a int, b char(20));")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE with_sequence")
			testutils.AssertQueryRuns(connection, "CREATE SEQUENCE my_sequence OWNED BY with_sequence.a;")
			defer testutils.AssertQueryRuns(connection, "DROP SEQUENCE my_sequence")

			sequenceMap := backup.GetSequenceOwnerMap(connection)

			Expect(len(sequenceMap)).To(Equal(1))
			Expect(sequenceMap["public.my_sequence"]).To(Equal("with_sequence.a"))
		})
	})
	Describe("GetDistributionPolicy", func() {
		It("returns a slice for a table DISTRIBUTED RANDOMLY", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE with_random_dist(a int, b char(20)) DISTRIBUTED RANDOMLY")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE with_random_dist")
			oid := testutils.OidFromRelationName(connection, "with_random_dist")

			result := backup.GetDistributionPolicy(connection, oid)

			Expect(result).To(Equal("DISTRIBUTED RANDOMLY"))
		})
		It("returns a slice for a table DISTRIBUTED BY one column", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE with_single_dist(a int, b char(20)) DISTRIBUTED BY (a)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE with_single_dist")
			oid := testutils.OidFromRelationName(connection, "with_single_dist")

			result := backup.GetDistributionPolicy(connection, oid)

			Expect(result).To(Equal("DISTRIBUTED BY (a)"))
		})
		It("returns a slice for a table DISTRIBUTED BY two columns", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE with_multiple_dist(a int, b char(20)) DISTRIBUTED BY (a, b)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE with_multiple_dist")
			oid := testutils.OidFromRelationName(connection, "with_multiple_dist")

			result := backup.GetDistributionPolicy(connection, oid)

			Expect(result).To(Equal("DISTRIBUTED BY (a, b)"))
		})
	})
	Describe("GetAllSequences", func() {
		It("returns a slice of definitions for all sequences", func() {
			testutils.AssertQueryRuns(connection, "CREATE SEQUENCE seq_one START 3")
			defer testutils.AssertQueryRuns(connection, "DROP SEQUENCE seq_one")
			testutils.AssertQueryRuns(connection, "COMMENT ON SEQUENCE public.seq_one IS 'this is a sequence comment'")

			testutils.AssertQueryRuns(connection, "CREATE SEQUENCE seq_two START 7")
			defer testutils.AssertQueryRuns(connection, "DROP SEQUENCE seq_two")

			seqOneRelation := utils.Relation{SchemaName: "public", RelationName: "seq_one", Comment: "this is a sequence comment", Owner: "testrole"}
			seqOneDef := backup.QuerySequenceDefinition{Name: "seq_one", LastVal: 3, Increment: 1, MaxVal: 9223372036854775807, MinVal: 1, CacheVal: 1}
			seqTwoRelation := utils.Relation{SchemaName: "public", RelationName: "seq_two", Owner: "testrole"}
			seqTwoDef := backup.QuerySequenceDefinition{Name: "seq_two", LastVal: 7, Increment: 1, MaxVal: 9223372036854775807, MinVal: 1, CacheVal: 1}

			results := backup.GetAllSequences(connection)

			testutils.ExpectStructsToMatchExcluding(&seqOneRelation, &results[0].Relation, "SchemaOid", "RelationOid")
			testutils.ExpectStructsToMatchExcluding(&seqOneDef, &results[0].QuerySequenceDefinition)
			testutils.ExpectStructsToMatchExcluding(&seqTwoRelation, &results[1].Relation, "SchemaOid", "RelationOid")
			testutils.ExpectStructsToMatchExcluding(&seqTwoDef, &results[1].QuerySequenceDefinition)
		})
	})
	Describe("GetSequenceDefinition", func() {
		It("returns a slice for a sequence", func() {
			testutils.AssertQueryRuns(connection, `CREATE SEQUENCE mysequence
MAXVALUE 1000
CACHE 41
START 42
CYCLE`)
			defer testutils.AssertQueryRuns(connection, "DROP SEQUENCE mysequence")
			testutils.AssertQueryRuns(connection, "COMMENT ON SEQUENCE public.mysequence IS 'this is a sequence comment'")

			expectedSequenceDef := backup.QuerySequenceDefinition{Name: "mysequence", LastVal: 42, Increment: 1, MaxVal: 1000, MinVal: 1, CacheVal: 41, IsCycled: true}

			result := backup.GetSequenceDefinition(connection, "mysequence")

			testutils.ExpectStructsToMatch(&expectedSequenceDef, &result)
		})
	})
	Describe("GetSessionGUCs", func() {
		It("returns a slice of values for session level GUCs", func() {
			/*
			 * We shouldn't need to run any setup queries, because we're using
			 * the default values for GPDB 5.
			 */
			results := backup.GetSessionGUCs(connection)
			Expect(results.ClientEncoding).To(Equal("UTF8"))
			Expect(results.StdConformingStrings).To(Equal("on"))
			Expect(results.DefaultWithOids).To(Equal("off"))
		})
	})
	Describe("ConstructImplicitIndexNames", func() {
		It("returns an empty map if there are no implicit indexes", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE simple_table(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE simple_table")

			indexNameMap := backup.ConstructImplicitIndexNames(connection)

			Expect(len(indexNameMap)).To(Equal(0))
		})
		It("returns a map of all implicit indexes", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE simple_table(i int UNIQUE)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE simple_table")

			indexNameMap := backup.ConstructImplicitIndexNames(connection)

			Expect(len(indexNameMap)).To(Equal(1))
			Expect(indexNameMap["public.simple_table_i_key"]).To(BeTrue())
		})
	})
	Describe("GetIndexMetadata", func() {
		var indexNameMap map[string]bool
		BeforeEach(func() {
			indexNameMap = make(map[string]bool, 0)
		})
		It("returns no slice when no index exists", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE simple_table(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE simple_table")
			oid := testutils.OidFromRelationName(connection, "simple_table")

			results := backup.GetIndexMetadata(connection, oid, indexNameMap)

			Expect(len(results)).To(Equal(0))
		})
		It("returns a slice of multiple indexes", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE simple_table(i int, j int, k int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE simple_table")
			testutils.AssertQueryRuns(connection, "CREATE INDEX simple_table_idx1 ON simple_table(i)")
			defer testutils.AssertQueryRuns(connection, "DROP INDEX simple_table_idx1")
			testutils.AssertQueryRuns(connection, "CREATE INDEX simple_table_idx2 ON simple_table(j)")
			defer testutils.AssertQueryRuns(connection, "DROP INDEX simple_table_idx2")
			testutils.AssertQueryRuns(connection, "COMMENT ON INDEX simple_table_idx2 IS 'this is a index comment'")
			oid := testutils.OidFromRelationName(connection, "simple_table")

			index1 := backup.QuerySimpleDefinition{"simple_table_idx1", "public", "simple_table",
				"CREATE INDEX simple_table_idx1 ON simple_table USING btree (i)", ""}
			index2 := backup.QuerySimpleDefinition{"simple_table_idx2", "public", "simple_table",
				"CREATE INDEX simple_table_idx2 ON simple_table USING btree (j)", "this is a index comment"}

			results := backup.GetIndexMetadata(connection, oid, indexNameMap)

			Expect(len(results)).To(Equal(2))
			testutils.ExpectStructsToMatch(&index1, &results[0])
			testutils.ExpectStructsToMatch(&index2, &results[1])
		})
		It("returns a slice of multiple indexes, excluding implicit indexes", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE simple_table(i int UNIQUE, j int, k int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE simple_table")
			testutils.AssertQueryRuns(connection, "CREATE INDEX simple_table_idx1 ON simple_table(i)")
			defer testutils.AssertQueryRuns(connection, "DROP INDEX simple_table_idx1")
			testutils.AssertQueryRuns(connection, "CREATE INDEX simple_table_idx2 ON simple_table(j)")
			defer testutils.AssertQueryRuns(connection, "DROP INDEX simple_table_idx2")
			testutils.AssertQueryRuns(connection, "COMMENT ON INDEX simple_table_idx2 IS 'this is a index comment'")
			oid := testutils.OidFromRelationName(connection, "simple_table")
			indexNameMap["public.simple_table_i_key"] = true

			index1 := backup.QuerySimpleDefinition{"simple_table_idx1", "public", "simple_table",
				"CREATE INDEX simple_table_idx1 ON simple_table USING btree (i)", ""}
			index2 := backup.QuerySimpleDefinition{"simple_table_idx2", "public", "simple_table",
				"CREATE INDEX simple_table_idx2 ON simple_table USING btree (j)", "this is a index comment"}

			results := backup.GetIndexMetadata(connection, oid, indexNameMap)

			Expect(len(results)).To(Equal(2))
			testutils.ExpectStructsToMatch(&index1, &results[0])
			testutils.ExpectStructsToMatch(&index2, &results[1])
		})
	})
	Describe("GetRuleMetadata", func() {
		It("returns no slice when no rule exists", func() {
			results := backup.GetRuleMetadata(connection)

			Expect(len(results)).To(Equal(0))
		})
		It("returns a slice of multiple rules", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE rule_table1(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE rule_table1")
			testutils.AssertQueryRuns(connection, "CREATE TABLE rule_table2(j int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE rule_table2")
			testutils.AssertQueryRuns(connection, "CREATE RULE double_insert AS ON INSERT TO rule_table1 DO INSERT INTO rule_table2 DEFAULT VALUES")
			defer testutils.AssertQueryRuns(connection, "DROP RULE double_insert ON rule_table1")
			testutils.AssertQueryRuns(connection, "CREATE RULE update_notify AS ON UPDATE TO rule_table1 DO NOTIFY rule_table1")
			defer testutils.AssertQueryRuns(connection, "DROP RULE update_notify ON rule_table1")
			testutils.AssertQueryRuns(connection, "COMMENT ON RULE update_notify ON rule_table1 IS 'This is a rule comment.'")

			rule1 := backup.QuerySimpleDefinition{"double_insert", "public", "rule_table1",
				"CREATE RULE double_insert AS ON INSERT TO rule_table1 DO INSERT INTO rule_table2 DEFAULT VALUES;", ""}
			rule2 := backup.QuerySimpleDefinition{"update_notify", "public", "rule_table1",
				"CREATE RULE update_notify AS ON UPDATE TO rule_table1 DO NOTIFY rule_table1;", "This is a rule comment."}

			results := backup.GetRuleMetadata(connection)

			Expect(len(results)).To(Equal(2))
			testutils.ExpectStructsToMatch(&rule1, &results[0])
			testutils.ExpectStructsToMatch(&rule2, &results[1])
		})
	})
	Describe("GetTriggerMetadata", func() {
		It("returns no slice when no trigger exists", func() {
			results := backup.GetTriggerMetadata(connection)

			Expect(len(results)).To(Equal(0))
		})
		It("returns a slice of multiple triggers", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE trigger_table1(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE trigger_table1")
			testutils.AssertQueryRuns(connection, "CREATE TABLE trigger_table2(j int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE trigger_table2")
			testutils.AssertQueryRuns(connection, "CREATE TRIGGER sync_trigger_table1 AFTER INSERT OR DELETE OR UPDATE ON trigger_table1 FOR EACH STATEMENT EXECUTE PROCEDURE flatfile_update_trigger()")
			defer testutils.AssertQueryRuns(connection, "DROP TRIGGER sync_trigger_table1 ON trigger_table1")
			testutils.AssertQueryRuns(connection, "CREATE TRIGGER sync_trigger_table2 AFTER INSERT OR DELETE OR UPDATE ON trigger_table2 FOR EACH STATEMENT EXECUTE PROCEDURE flatfile_update_trigger()")
			defer testutils.AssertQueryRuns(connection, "DROP TRIGGER sync_trigger_table2 ON trigger_table2")
			testutils.AssertQueryRuns(connection, "COMMENT ON TRIGGER sync_trigger_table2 ON trigger_table2 IS 'This is a trigger comment.'")

			trigger1 := backup.QuerySimpleDefinition{"sync_trigger_table1", "public", "trigger_table1",
				"CREATE TRIGGER sync_trigger_table1 AFTER INSERT OR DELETE OR UPDATE ON trigger_table1 FOR EACH STATEMENT EXECUTE PROCEDURE flatfile_update_trigger()",
				""}
			trigger2 := backup.QuerySimpleDefinition{"sync_trigger_table2", "public", "trigger_table2",
				"CREATE TRIGGER sync_trigger_table2 AFTER INSERT OR DELETE OR UPDATE ON trigger_table2 FOR EACH STATEMENT EXECUTE PROCEDURE flatfile_update_trigger()",
				"This is a trigger comment."}

			results := backup.GetTriggerMetadata(connection)

			Expect(len(results)).To(Equal(2))
			testutils.ExpectStructsToMatch(&trigger1, &results[0])
			testutils.ExpectStructsToMatch(&trigger2, &results[1])
		})
		It("does not include constraint triggers", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE trigger_table1(i int PRIMARY KEY)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE trigger_table1")
			testutils.AssertQueryRuns(connection, "CREATE TABLE trigger_table2(j int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE trigger_table2")
			testutils.AssertQueryRuns(connection, "ALTER TABLE trigger_table2 ADD CONSTRAINT fkc FOREIGN KEY (j) REFERENCES trigger_table1 (i) ON UPDATE RESTRICT ON DELETE RESTRICT")

			results := backup.GetTriggerMetadata(connection)

			Expect(len(results)).To(Equal(0))
		})
	})
	Describe("GetDatabaseComment", func() {
		It("returns empty string for a database comment", func() {
			result := backup.GetDatabaseComment(connection)
			Expect(result).To(Equal(""))
		})
		It("returns a value for a database comment", func() {
			testutils.AssertQueryRuns(connection, "COMMENT ON DATABASE testdb IS 'this is a database comment'")
			defer testutils.AssertQueryRuns(connection, "COMMENT ON DATABASE testdb IS NULL")
			result := backup.GetDatabaseComment(connection)
			Expect(result).To(Equal("this is a database comment"))
		})
	})
	Describe("GetProceduralLanguages", func() {
		It("returns a slice of procedural languages", func() {
			testutils.AssertQueryRuns(connection, "CREATE LANGUAGE plpythonu")
			defer testutils.AssertQueryRuns(connection, "DROP LANGUAGE plpythonu")

			pgsqlHandlerOid := testutils.OidFromFunctionName(connection, "plpgsql_call_handler")
			pgsqlInlineOid := testutils.OidFromFunctionName(connection, "plpgsql_inline_handler")
			pgsqlValidatorOid := testutils.OidFromFunctionName(connection, "plpgsql_validator")

			pythonHandlerOid := testutils.OidFromFunctionName(connection, "plpython_call_handler")
			pythonInlineOid := testutils.OidFromFunctionName(connection, "plpython_inline_handler")

			expectedPlpgsqlInfo := backup.QueryProceduralLanguage{"plpgsql", "testrole", true, true, pgsqlHandlerOid, pgsqlInlineOid, pgsqlValidatorOid, "", ""}
			expectedPlpythonuInfo := backup.QueryProceduralLanguage{"plpythonu", "testrole", true, false, pythonHandlerOid, pythonInlineOid, 0, "", ""}

			resultProcLangs := backup.GetProceduralLanguages(connection)

			Expect(len(resultProcLangs)).To(Equal(2))
			testutils.ExpectStructsToMatchExcluding(&expectedPlpgsqlInfo, &resultProcLangs[0], "Owner")
			testutils.ExpectStructsToMatch(&resultProcLangs[1], &expectedPlpythonuInfo)
		})
	})
	Describe("GetTypeDefinitions", func() {
		var (
			shellType         backup.TypeDefinition
			baseTypeDefault   backup.TypeDefinition
			baseTypeCustom    backup.TypeDefinition
			compositeTypeAtt1 backup.TypeDefinition
			compositeTypeAtt2 backup.TypeDefinition
			compositeTypeAtt3 backup.TypeDefinition
			enumType          backup.TypeDefinition
		)
		BeforeEach(func() {
			shellType = backup.TypeDefinition{Type: "p", TypeSchema: "public", TypeName: "shell_type"}
			baseTypeDefault = backup.TypeDefinition{
				Type: "b", TypeSchema: "public", TypeName: "base_type", Input: "base_fn_in", Output: "base_fn_out", Receive: "-",
				Send: "-", ModIn: "-", ModOut: "-", InternalLength: -1, IsPassedByValue: false, Alignment: "i", Storage: "p",
				DefaultVal: "", Element: "-", Delimiter: ",", Comment: "", Owner: "testrole",
			}
			baseTypeCustom = backup.TypeDefinition{
				Type: "b", TypeSchema: "public", TypeName: "base_type", Input: "base_fn_in", Output: "base_fn_out", Receive: "-",
				Send: "-", ModIn: "-", ModOut: "-", InternalLength: 8, IsPassedByValue: true, Alignment: "c", Storage: "p",
				DefaultVal: "0", Element: "integer", Delimiter: ";", Comment: "this is a type comment", Owner: "testrole",
			}
			compositeTypeAtt1 = backup.TypeDefinition{
				Type: "c", TypeSchema: "public", TypeName: "composite_type", Comment: "", Owner: "testrole",
				AttName: "name", AttType: "integer",
			}
			compositeTypeAtt2 = backup.TypeDefinition{
				Type: "c", TypeSchema: "public", TypeName: "composite_type", Comment: "", Owner: "testrole",
				AttName: "name1", AttType: "integer",
			}
			compositeTypeAtt3 = backup.TypeDefinition{
				Type: "c", TypeSchema: "public", TypeName: "composite_type", Comment: "", Owner: "testrole",
				AttName: "name2", AttType: "text",
			}
			enumType = backup.TypeDefinition{
				Type: "e", TypeSchema: "public", TypeName: "enum_type", AttName: "", AttType: "", Input: "enum_in", Output: "enum_out",
				Receive: "enum_recv", Send: "enum_send", ModIn: "-", ModOut: "-", InternalLength: 4, IsPassedByValue: true,
				Alignment: "i", Storage: "p", DefaultVal: "", Element: "-", Delimiter: ",", EnumLabels: "'label1',\n\t'label2',\n\t'label3'",
				Comment: "", Owner: "testrole",
			}
		})
		It("returns a slice for a shell type", func() {
			testutils.AssertQueryRuns(connection, "CREATE TYPE shell_type")
			defer testutils.AssertQueryRuns(connection, "DROP TYPE shell_type")

			results := backup.GetTypeDefinitions(connection)

			Expect(len(results)).To(Equal(1))
			testutils.ExpectStructsToMatchIncluding(&shellType, &results[0], "TypeSchema", "TypeName", "Type")
		})
		It("returns a slice of composite types", func() {
			testutils.AssertQueryRuns(connection, "CREATE TYPE composite_type AS (name int4, name1 int, name2 text);")
			defer testutils.AssertQueryRuns(connection, "DROP TYPE composite_type")

			results := backup.GetTypeDefinitions(connection)

			Expect(len(results)).To(Equal(3))
			testutils.ExpectStructsToMatchIncluding(&compositeTypeAtt1, &results[0], "Type", "TypeSchema", "TypeName", "Comment", "Owner", "AttName", "AttType")
			testutils.ExpectStructsToMatchIncluding(&compositeTypeAtt2, &results[1], "Type", "TypeSchema", "TypeName", "Comment", "Owner", "AttName", "AttType")
			testutils.ExpectStructsToMatchIncluding(&compositeTypeAtt3, &results[2], "Type", "TypeSchema", "TypeName", "Comment", "Owner", "AttName", "AttType")
		})
		It("returns a slice for a base type with default values", func() {
			testutils.AssertQueryRuns(connection, "CREATE TYPE base_type")
			defer testutils.AssertQueryRuns(connection, "DROP TYPE base_type CASCADE")
			testutils.AssertQueryRuns(connection, "CREATE FUNCTION base_fn_in(cstring) RETURNS base_type AS 'boolin' LANGUAGE internal")
			testutils.AssertQueryRuns(connection, "CREATE FUNCTION base_fn_out(base_type) RETURNS cstring AS 'boolout' LANGUAGE internal")
			testutils.AssertQueryRuns(connection, "CREATE TYPE base_type(INPUT=base_fn_in, OUTPUT=base_fn_out)")

			results := backup.GetTypeDefinitions(connection)

			Expect(len(results)).To(Equal(1))
			testutils.ExpectStructsToMatch(&results[0], &baseTypeDefault)
		})
		It("returns a slice for a base type with custom configuration", func() {
			testutils.AssertQueryRuns(connection, "CREATE TYPE base_type")
			defer testutils.AssertQueryRuns(connection, "DROP TYPE base_type CASCADE")
			testutils.AssertQueryRuns(connection, "CREATE FUNCTION base_fn_in(cstring) RETURNS base_type AS 'boolin' LANGUAGE internal")
			testutils.AssertQueryRuns(connection, "CREATE FUNCTION base_fn_out(base_type) RETURNS cstring AS 'boolout' LANGUAGE internal")
			testutils.AssertQueryRuns(connection, "CREATE TYPE base_type(INPUT=base_fn_in, OUTPUT=base_fn_out, INTERNALLENGTH=8, PASSEDBYVALUE, ALIGNMENT=char, STORAGE=plain, DEFAULT=0, ELEMENT=integer, DELIMITER=';')")
			testutils.AssertQueryRuns(connection, "COMMENT ON TYPE base_type IS 'this is a type comment'")

			results := backup.GetTypeDefinitions(connection)

			Expect(len(results)).To(Equal(1))
			testutils.ExpectStructsToMatch(&results[0], &baseTypeCustom)
		})
		It("returns a slice for an enum type", func() {
			testutils.AssertQueryRuns(connection, "CREATE TYPE enum_type AS ENUM ('label1','label2','label3')")
			defer testutils.AssertQueryRuns(connection, "DROP TYPE enum_type")

			results := backup.GetTypeDefinitions(connection)

			Expect(len(results)).To(Equal(1))
			testutils.ExpectStructsToMatch(&results[0], &enumType)
		})
		It("returns a slice containing information for a mix of types", func() {
			testutils.AssertQueryRuns(connection, "CREATE TYPE shell_type")
			defer testutils.AssertQueryRuns(connection, "DROP TYPE shell_type")
			testutils.AssertQueryRuns(connection, "CREATE TYPE composite_type AS (name int4, name1 int, name2 text);")
			defer testutils.AssertQueryRuns(connection, "DROP TYPE composite_type")
			testutils.AssertQueryRuns(connection, "CREATE TYPE base_type")
			defer testutils.AssertQueryRuns(connection, "DROP TYPE base_type CASCADE")
			testutils.AssertQueryRuns(connection, "CREATE FUNCTION base_fn_in(cstring) RETURNS base_type AS 'boolin' LANGUAGE internal")
			testutils.AssertQueryRuns(connection, "CREATE FUNCTION base_fn_out(base_type) RETURNS cstring AS 'boolout' LANGUAGE internal")
			testutils.AssertQueryRuns(connection, "CREATE TYPE base_type(INPUT=base_fn_in, OUTPUT=base_fn_out, INTERNALLENGTH=8, PASSEDBYVALUE, ALIGNMENT=char, STORAGE=plain, DEFAULT=0, ELEMENT=integer, DELIMITER=';')")
			testutils.AssertQueryRuns(connection, "COMMENT ON TYPE base_type IS 'this is a type comment'")
			testutils.AssertQueryRuns(connection, "CREATE TYPE enum_type AS ENUM ('label1','label2','label3')")
			defer testutils.AssertQueryRuns(connection, "DROP TYPE enum_type")

			resultTypes := backup.GetTypeDefinitions(connection)

			Expect(len(resultTypes)).To(Equal(6))
			testutils.ExpectStructsToMatch(&resultTypes[0], &baseTypeCustom)
			testutils.ExpectStructsToMatchIncluding(&compositeTypeAtt1, &resultTypes[1], "Type", "TypeSchema", "TypeName", "Comment", "Owner", "AttName", "AttType")
			testutils.ExpectStructsToMatchIncluding(&compositeTypeAtt2, &resultTypes[2], "Type", "TypeSchema", "TypeName", "Comment", "Owner", "AttName", "AttType")
			testutils.ExpectStructsToMatchIncluding(&compositeTypeAtt3, &resultTypes[3], "Type", "TypeSchema", "TypeName", "Comment", "Owner", "AttName", "AttType")
			testutils.ExpectStructsToMatch(&resultTypes[4], &enumType)
			testutils.ExpectStructsToMatchIncluding(&shellType, &resultTypes[5], "TypeSchema", "TypeName", "Type")
		})
		It("does not return types for sequences or views", func() {
			testutils.AssertQueryRuns(connection, "CREATE SEQUENCE my_sequence START 10")
			defer testutils.AssertQueryRuns(connection, "DROP SEQUENCE my_sequence")
			testutils.AssertQueryRuns(connection, "CREATE VIEW simpleview AS SELECT rolname FROM pg_roles")
			defer testutils.AssertQueryRuns(connection, "DROP VIEW simpleview")

			results := backup.GetTypeDefinitions(connection)

			Expect(len(results)).To(Equal(0))
		})
	})
	Describe("GetMetadataForObjectType", func() {
		It("returns a slice of metadata with modified privileges", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE foo(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE foo")
			testutils.AssertQueryRuns(connection, "REVOKE DELETE ON TABLE foo FROM testrole")
			testutils.AssertQueryRuns(connection, "CREATE TABLE bar(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE bar")
			testutils.AssertQueryRuns(connection, "REVOKE ALL ON TABLE bar FROM testrole")
			testutils.AssertQueryRuns(connection, "CREATE TABLE baz(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE baz")
			testutils.AssertQueryRuns(connection, "GRANT ALL ON TABLE baz TO gpadmin")

			resultMetadataMap := backup.GetMetadataForObjectType(connection, "relnamespace", "relacl", "relowner", "pg_class")

			fooOid := testutils.OidFromRelationName(connection, "foo")
			barOid := testutils.OidFromRelationName(connection, "bar")
			bazOid := testutils.OidFromRelationName(connection, "baz")
			expectedFoo := utils.ObjectMetadata{Privileges: []utils.ACL{
				{"testrole", true, true, true, false, true, true, true},
			}, Owner: "testrole"}
			expectedBar := utils.ObjectMetadata{Privileges: []utils.ACL{
				{Grantee: "GRANTEE"},
			}, Owner: "testrole"}
			expectedBaz := utils.ObjectMetadata{Privileges: []utils.ACL{
				{"gpadmin", true, true, true, true, true, true, true},
				{"testrole", true, true, true, true, true, true, true},
			}, Owner: "testrole"}
			Expect(len(resultMetadataMap)).To(Equal(3))
			resultFoo := resultMetadataMap[fooOid]
			resultBar := resultMetadataMap[barOid]
			resultBaz := resultMetadataMap[bazOid]
			testutils.ExpectStructsToMatch(&resultFoo, &expectedFoo)
			testutils.ExpectStructsToMatch(&resultBar, &expectedBar)
			testutils.ExpectStructsToMatch(&resultBaz, &expectedBaz)
		})
		It("returns a slice of default metadata for a table", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE testtable(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE testtable")
			testutils.AssertQueryRuns(connection, "COMMENT ON TABLE testtable IS 'This is a table comment.'")

			resultMetadataMap := backup.GetMetadataForObjectType(connection, "relnamespace", "relacl", "relowner", "pg_class")

			oid := testutils.OidFromRelationName(connection, "testtable")
			expectedMetadata := utils.ObjectMetadata{Privileges: []utils.ACL{}, Owner: "testrole", Comment: "This is a table comment."}
			Expect(len(resultMetadataMap)).To(Equal(1))
			resultMetadata := resultMetadataMap[oid]
			testutils.ExpectStructsToMatch(&resultMetadata, &expectedMetadata)
		})
	})
	Describe("GetExternalTablesMap", func() {
		It("returns empty map when there are no external tables", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE simple_table(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE simple_table")

			result := backup.GetExternalTablesMap(connection)

			Expect(len(result)).To(Equal(0))
		})
		It("returns map with external tables", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE simple_table(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE simple_table")
			testutils.AssertQueryRuns(connection, `CREATE READABLE EXTERNAL TABLE ext_table(i int)
LOCATION ('file://tmp/myfile.txt')
FORMAT 'TEXT' ( DELIMITER '|' NULL ' ')`)
			defer testutils.AssertQueryRuns(connection, "DROP EXTERNAL TABLE ext_table")

			result := backup.GetExternalTablesMap(connection)

			Expect(len(result)).To(Equal(1))
			Expect(result["public.ext_table"]).To(BeTrue())
		})
		// TODO: Add tests for external partitions
	})
	Describe("GetExternalTableDefinition", func() {
		It("returns a slice for a basic external table definition", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE simple_table(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE simple_table")
			testutils.AssertQueryRuns(connection, `CREATE READABLE EXTERNAL TABLE ext_table(i int)
LOCATION ('file://tmp/myfile.txt')
FORMAT 'TEXT'`)
			defer testutils.AssertQueryRuns(connection, "DROP EXTERNAL TABLE ext_table")
			oid := testutils.OidFromRelationName(connection, "ext_table")

			result := backup.GetExternalTableDefinition(connection, oid)

			extTable := backup.ExternalTableDefinition{
				0, 0, "file://tmp/myfile.txt", "ALL_SEGMENTS",
				"t", "delimiter '	' null '\\N' escape '\\'", "", "",
				0, "", "", "UTF8", false,
			}

			testutils.ExpectStructsToMatchExcluding(&extTable, &result)
		})
		It("returns a slice for a complex external table definition", func() {
			testutils.AssertQueryRuns(connection, `CREATE READABLE EXTERNAL TABLE ext_table(i int)
LOCATION ('file://tmp/myfile.txt')
FORMAT 'TEXT'
OPTIONS (foo 'bar')
LOG ERRORS
SEGMENT REJECT LIMIT 10 PERCENT
`)
			defer testutils.AssertQueryRuns(connection, "DROP EXTERNAL TABLE ext_table")
			oid := testutils.OidFromRelationName(connection, "ext_table")

			result := backup.GetExternalTableDefinition(connection, oid)

			extTable := backup.ExternalTableDefinition{
				0, 0, "file://tmp/myfile.txt", "ALL_SEGMENTS",
				"t", "delimiter '	' null '\\N' escape '\\'", "foo 'bar'", "",
				10, "p", "ext_table", "UTF8", false,
			}

			testutils.ExpectStructsToMatchExcluding(&extTable, &result)
		})
		// TODO: Add tests for external partitions
	})
	Describe("GetDatabaseGUCs", func() {
		It("returns a slice of values for database level GUCs", func() {
			testutils.AssertQueryRuns(connection, "ALTER DATABASE testdb SET default_with_oids TO true")
			defer testutils.AssertQueryRuns(connection, "ALTER DATABASE testdb SET default_with_oids TO false")
			testutils.AssertQueryRuns(connection, "ALTER DATABASE testdb SET search_path TO public,pg_catalog")
			defer testutils.AssertQueryRuns(connection, "ALTER DATABASE testdb SET search_path TO pg_catalog,public")
			results := backup.GetDatabaseGUCs(connection)
			Expect(len(results)).To(Equal(2))
			Expect(results[0]).To(Equal("SET default_with_oids TO true"))
			Expect(results[1]).To(Equal("SET search_path TO public, pg_catalog"))
		})
	})
	Describe("GetDatabaseOwner", func() {
		It("returns a value for database owner", func() {
			result := backup.GetDatabaseOwner(connection)
			Expect(result).To(Equal("gpadmin"))
		})
	})
	Describe("GetPartitionDefinition", func() {
		It("returns empty string when no partition exists", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE simple_table(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE simple_table")
			oid := testutils.OidFromRelationName(connection, "simple_table")

			result := backup.GetPartitionDefinition(connection, oid)

			Expect(result).To(Equal(""))
		})
		It("returns a value for a partition definition", func() {
			testutils.AssertQueryRuns(connection, `CREATE TABLE part_table (id int, rank int, year int, gender 
char(1), count int ) 
DISTRIBUTED BY (id)
PARTITION BY LIST (gender)
( PARTITION girls VALUES ('F'), 
  PARTITION boys VALUES ('M'), 
  DEFAULT PARTITION other );
			`)
			defer testutils.AssertQueryRuns(connection, "DROP TABLE part_table")
			oid := testutils.OidFromRelationName(connection, "part_table")

			result := backup.GetPartitionDefinition(connection, oid)

			// The spacing is very specific here and is output from the postgres function
			expectedResult := `PARTITION BY LIST(gender) 
          (
          PARTITION girls VALUES('F') WITH (tablename='part_table_1_prt_girls', appendonly=false ), 
          PARTITION boys VALUES('M') WITH (tablename='part_table_1_prt_boys', appendonly=false ), 
          DEFAULT PARTITION other  WITH (tablename='part_table_1_prt_other', appendonly=false )
          )`
			Expect(result).To(Equal(expectedResult))
		})
	})
	Describe("GetPartitionDefinitionTemplate", func() {
		It("returns empty string when no partition definition template exists", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE simple_table(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE simple_table")
			oid := testutils.OidFromRelationName(connection, "simple_table")

			result := backup.GetPartitionTemplateDefinition(connection, oid)

			Expect(result).To(Equal(""))
		})
		It("returns a value for a subpartition template", func() {
			testutils.AssertQueryRuns(connection, `CREATE TABLE part_table (trans_id int, date date, amount decimal(9,2), region text)
  DISTRIBUTED BY (trans_id)
  PARTITION BY RANGE (date)
  SUBPARTITION BY LIST (region)
  SUBPARTITION TEMPLATE
    ( SUBPARTITION usa VALUES ('usa'),
      SUBPARTITION asia VALUES ('asia'),
      SUBPARTITION europe VALUES ('europe'),
      DEFAULT SUBPARTITION other_regions )
  ( START (date '2014-01-01') INCLUSIVE
    END (date '2014-04-01') EXCLUSIVE
    EVERY (INTERVAL '1 month') ) `)
			defer testutils.AssertQueryRuns(connection, "DROP TABLE part_table")
			oid := testutils.OidFromRelationName(connection, "part_table")

			result := backup.GetPartitionTemplateDefinition(connection, oid)

			// The spacing is very specific here and is output from the postgres function
			expectedResult := `ALTER TABLE part_table 
SET SUBPARTITION TEMPLATE  
          (
          SUBPARTITION usa VALUES('usa') WITH (tablename='part_table'), 
          SUBPARTITION asia VALUES('asia') WITH (tablename='part_table'), 
          SUBPARTITION europe VALUES('europe') WITH (tablename='part_table'), 
          DEFAULT SUBPARTITION other_regions  WITH (tablename='part_table')
          )
`

			Expect(result).To(Equal(expectedResult))
		})
	})
	Describe("GetStorageOptions", func() {
		It("returns an empty string when no table storage options exist ", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE simple_table(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE simple_table")
			oid := testutils.OidFromRelationName(connection, "simple_table")

			result := backup.GetStorageOptions(connection, oid)

			Expect(result).To(Equal(""))
		})
		It("returns a value for storage options of a table ", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE ao_table(i int) with (appendonly=true)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE ao_table")
			oid := testutils.OidFromRelationName(connection, "ao_table")

			result := backup.GetStorageOptions(connection, oid)

			Expect(result).To(Equal("appendonly=true"))
		})
	})
	Describe("GetFunctionDefinitions", func() {
		It("returns a slice of function definitions", func() {
			testutils.AssertQueryRuns(connection, `CREATE FUNCTION add(integer, integer) RETURNS integer
AS 'SELECT $1 + $2'
LANGUAGE SQL`)
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION add(integer, integer)")
			testutils.AssertQueryRuns(connection, `
CREATE FUNCTION append(integer, integer) RETURNS SETOF record
AS 'SELECT ($1, $2)'
LANGUAGE SQL
SECURITY DEFINER
STRICT
STABLE
COST 200
ROWS 200
SET search_path = pg_temp
MODIFIES SQL DATA
`)
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION append(integer, integer)")
			testutils.AssertQueryRuns(connection, "COMMENT ON FUNCTION append(integer, integer) IS 'this is a function comment'")

			results := backup.GetFunctionDefinitions(connection)

			addFunction := backup.QueryFunctionDefinition{
				SchemaName: "public", FunctionName: "add", ReturnsSet: false, FunctionBody: "SELECT $1 + $2",
				BinaryPath: "", Arguments: "integer, integer", IdentArgs: "integer, integer", ResultType: "integer",
				Volatility: "v", IsStrict: false, IsSecurityDefiner: false, Config: "", Cost: 100, NumRows: 0, DataAccess: "c",
				Language: "sql", Comment: "", Owner: "testrole"}
			appendFunction := backup.QueryFunctionDefinition{
				SchemaName: "public", FunctionName: "append", ReturnsSet: true, FunctionBody: "SELECT ($1, $2)",
				BinaryPath: "", Arguments: "integer, integer", IdentArgs: "integer, integer", ResultType: "SETOF record",
				Volatility: "s", IsStrict: true, IsSecurityDefiner: true, Config: "SET search_path TO pg_temp", Cost: 200,
				NumRows: 200, DataAccess: "m", Language: "sql", Comment: "this is a function comment", Owner: "testrole"}

			Expect(len(results)).To(Equal(2))
			testutils.ExpectStructsToMatch(&results[0], &addFunction)
			testutils.ExpectStructsToMatch(&results[1], &appendFunction)
		})
	})
	Describe("GetAggregateDefinitions", func() {
		It("returns a slice of aggregate definitions", func() {
			testutils.AssertQueryRuns(connection, `
CREATE FUNCTION mysfunc_accum(numeric, numeric, numeric)
   RETURNS numeric
   AS 'select $1 + $2 + $3'
   LANGUAGE SQL
   IMMUTABLE
   RETURNS NULL ON NULL INPUT;
`)
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION mysfunc_accum(numeric, numeric, numeric)")
			testutils.AssertQueryRuns(connection, `
CREATE FUNCTION mypre_accum(numeric, numeric)
   RETURNS numeric
   AS 'select $1 + $2'
   LANGUAGE SQL
   IMMUTABLE
   RETURNS NULL ON NULL INPUT;
`)
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION mypre_accum(numeric, numeric)")
			testutils.AssertQueryRuns(connection, `
CREATE AGGREGATE agg_prefunc(numeric, numeric) (
	SFUNC = mysfunc_accum,
	STYPE = numeric,
	PREFUNC = mypre_accum,
	INITCOND = 0 );
`)
			defer testutils.AssertQueryRuns(connection, "DROP AGGREGATE agg_prefunc(numeric, numeric)")

			transitionOid := testutils.OidFromFunctionName(connection, "mysfunc_accum")
			prelimOid := testutils.OidFromFunctionName(connection, "mypre_accum")

			result := backup.GetAggregateDefinitions(connection)

			aggregateDef := backup.QueryAggregateDefinition{
				SchemaName: "public", AggregateName: "agg_prefunc", Arguments: "numeric, numeric",
				IdentArgs: "numeric, numeric", TransitionFunction: transitionOid, PreliminaryFunction: prelimOid,
				FinalFunction: 0, SortOperator: 0, TransitionDataType: "numeric", InitialValue: "0", IsOrdered: false,
				Comment: "", Owner: "testrole",
			}

			Expect(len(result)).To(Equal(1))
			testutils.ExpectStructsToMatch(&result[0], &aggregateDef)
		})
	})
	Describe("GetFunctionOidToInfoMap", func() {
		It("returns map containing function information", func() {
			result := backup.GetFunctionOidToInfoMap(connection)
			initialLength := len(result)
			testutils.AssertQueryRuns(connection, `CREATE FUNCTION add(integer, integer) RETURNS integer
AS 'SELECT $1 + $2'
LANGUAGE SQL`)
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION add(integer, integer)")

			result = backup.GetFunctionOidToInfoMap(connection)
			oid := testutils.OidFromFunctionName(connection, "add")
			Expect(len(result)).To(Equal(initialLength + 1))
			Expect(result[oid].QualifiedName).To(Equal("public.add"))
			Expect(result[oid].Arguments).To(Equal("integer, integer"))
			Expect(result[oid].IsInternal).To(BeFalse())
		})
		It("returns a map containing an internal function", func() {
			result := backup.GetFunctionOidToInfoMap(connection)

			oid := testutils.OidFromFunctionName(connection, "boolin")
			Expect(result[oid].QualifiedName).To(Equal("pg_catalog.boolin"))
			Expect(result[oid].IsInternal).To(BeTrue())
		})
	})
	Describe("GetCastDefinitions", func() {
		It("returns a slice for a basic cast", func() {
			testutils.AssertQueryRuns(connection, "CREATE FUNCTION casttoint(text) RETURNS integer STRICT IMMUTABLE LANGUAGE SQL AS 'SELECT cast($1 as integer);'")
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION casttoint(text)")
			testutils.AssertQueryRuns(connection, "CREATE CAST (text AS integer) WITH FUNCTION casttoint(text) AS ASSIGNMENT")
			defer testutils.AssertQueryRuns(connection, "DROP CAST (text AS integer)")

			results := backup.GetCastDefinitions(connection)

			castDef := backup.QueryCastDefinition{SourceType: "text", TargetType: "integer", FunctionSchema: "public",
				FunctionName: "casttoint", FunctionArgs: "text", CastContext: "a", Comment: ""}

			Expect(len(results)).To(Equal(1))
			testutils.ExpectStructsToMatch(&castDef, &results[0])
		})
		It("returns a slice for a basic cast with comment", func() {
			testutils.AssertQueryRuns(connection, "CREATE FUNCTION casttoint(text) RETURNS integer STRICT IMMUTABLE LANGUAGE SQL AS 'SELECT cast($1 as integer);'")
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION casttoint(text)")
			testutils.AssertQueryRuns(connection, "CREATE CAST (text AS integer) WITH FUNCTION casttoint(text) AS ASSIGNMENT")
			defer testutils.AssertQueryRuns(connection, "DROP CAST (text AS integer)")
			testutils.AssertQueryRuns(connection, "COMMENT ON CAST (text AS integer) IS 'this is a cast comment'")

			results := backup.GetCastDefinitions(connection)

			castDef := backup.QueryCastDefinition{SourceType: "text", TargetType: "integer", FunctionSchema: "public",
				FunctionName: "casttoint", FunctionArgs: "text", CastContext: "a", Comment: "this is a cast comment"}

			Expect(len(results)).To(Equal(1))
			testutils.ExpectStructsToMatch(&castDef, &results[0])
		})
	})
	Describe("GetViewDefinitions", func() {
		It("returns a slice for a basic view", func() {
			testutils.AssertQueryRuns(connection, "CREATE VIEW simpleview AS SELECT rolname FROM pg_roles")
			defer testutils.AssertQueryRuns(connection, "DROP VIEW simpleview")

			results := backup.GetViewDefinitions(connection)

			viewDef := backup.QueryViewDefinition{"public", "simpleview", "SELECT pg_roles.rolname FROM pg_roles;", ""}

			Expect(len(results)).To(Equal(1))
			testutils.ExpectStructsToMatch(&viewDef, &results[0])
		})
		It("returns a slice for a view with a comment", func() {
			testutils.AssertQueryRuns(connection, "CREATE VIEW simpleview AS SELECT rolname FROM pg_roles")
			defer testutils.AssertQueryRuns(connection, "DROP VIEW simpleview")
			testutils.AssertQueryRuns(connection, "COMMENT ON VIEW simpleview IS 'this is a view comment'")

			results := backup.GetViewDefinitions(connection)

			viewDef := backup.QueryViewDefinition{"public", "simpleview", "SELECT pg_roles.rolname FROM pg_roles;", "this is a view comment"}

			Expect(len(results)).To(Equal(1))
			testutils.ExpectStructsToMatch(&viewDef, &results[0])
		})
	})
	Describe("GetExternalProtocols", func() {
		It("returns a slice for a protocol", func() {
			testutils.AssertQueryRuns(connection, "CREATE OR REPLACE FUNCTION write_to_s3() RETURNS integer AS '$libdir/gps3ext.so', 's3_export' LANGUAGE C STABLE;")
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION write_to_s3()")
			testutils.AssertQueryRuns(connection, "CREATE OR REPLACE FUNCTION read_from_s3() RETURNS integer AS '$libdir/gps3ext.so', 's3_import' LANGUAGE C STABLE;")
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION read_from_s3()")
			testutils.AssertQueryRuns(connection, "CREATE PROTOCOL s3 (writefunc = write_to_s3, readfunc = read_from_s3);")
			defer testutils.AssertQueryRuns(connection, "DROP PROTOCOL s3")

			readFunctionOid := testutils.OidFromFunctionName(connection, "read_from_s3")
			writeFunctionOid := testutils.OidFromFunctionName(connection, "write_to_s3")

			results := backup.GetExternalProtocols(connection)

			protocolDef := backup.QueryExtProtocol{"s3", "testrole", false, readFunctionOid, writeFunctionOid, 0, ""}

			Expect(len(results)).To(Equal(1))
			testutils.ExpectStructsToMatch(&protocolDef, &results[0])
		})
	})
	Describe("GetResourceQueues", func() {
		It("returns a slice for a resource queue with only ACTIVE_STATEMENTS", func() {
			testutils.AssertQueryRuns(connection, `CREATE RESOURCE QUEUE "statementsQueue" WITH (ACTIVE_STATEMENTS=7);`)
			defer testutils.AssertQueryRuns(connection, `DROP RESOURCE QUEUE "statementsQueue"`)

			results := backup.GetResourceQueues(connection)

			statementsQueue := backup.QueryResourceQueue{"statementsQueue", 7, "-1.00", false, "0.00", "medium", "-1", ""}

			//Since resource queues are global, we can't be sure this is the only one
			for _, resultQueue := range results {
				if resultQueue.Name == "statementsQueue" {
					testutils.ExpectStructsToMatch(&statementsQueue, &resultQueue)
					return
				}
			}
			Fail("Resource queue 'statementsQueue' was not found.")
		})
		It("returns a slice for a resource queue with only MAX_COST", func() {
			testutils.AssertQueryRuns(connection, `CREATE RESOURCE QUEUE "maxCostQueue" WITH (MAX_COST=32.8);`)
			defer testutils.AssertQueryRuns(connection, `DROP RESOURCE QUEUE "maxCostQueue"`)

			results := backup.GetResourceQueues(connection)

			maxCostQueue := backup.QueryResourceQueue{"maxCostQueue", -1, "32.80", false, "0.00", "medium", "-1", ""}

			for _, resultQueue := range results {
				if resultQueue.Name == "maxCostQueue" {
					testutils.ExpectStructsToMatch(&maxCostQueue, &resultQueue)
					return
				}
			}
			Fail("Resource queue 'maxCostQueue' was not found.")
		})
		It("returns a slice for a resource queue with everything", func() {
			testutils.AssertQueryRuns(connection, `CREATE RESOURCE QUEUE "commentQueue" WITH (ACTIVE_STATEMENTS=7, MAX_COST=3e+4, COST_OVERCOMMIT=TRUE, MIN_COST=22.53, PRIORITY=LOW, MEMORY_LIMIT='2GB');`)
			defer testutils.AssertQueryRuns(connection, `DROP RESOURCE QUEUE "commentQueue"`)
			testutils.AssertQueryRuns(connection, `COMMENT ON RESOURCE QUEUE "commentQueue" IS 'this is a resource queue comment'`)

			results := backup.GetResourceQueues(connection)

			commentQueue := backup.QueryResourceQueue{"commentQueue", 7, "30000.00", true, "22.53", "low", "2GB", "this is a resource queue comment"}

			for _, resultQueue := range results {
				if resultQueue.Name == "commentQueue" {
					testutils.ExpectStructsToMatch(&commentQueue, &resultQueue)
					return
				}
			}
			Fail("Resource queue 'commentsQueue' was not found.")
		})

	})
	Describe("GetDatabaseRoles", func() {
		It("returns a role with default properties", func() {
			testutils.AssertQueryRuns(connection, "CREATE ROLE role1 SUPERUSER NOINHERIT")
			defer testutils.AssertQueryRuns(connection, "DROP ROLE role1")

			results := backup.GetRoles(connection)

			roleOid := testutils.OidFromRoleName(connection, "role1")
			expectedRole := backup.QueryRole{
				Oid:             roleOid,
				Name:            "role1",
				Super:           true,
				Inherit:         false,
				CreateRole:      false,
				CreateDB:        false,
				CanLogin:        false,
				ConnectionLimit: -1,
				Password:        "",
				ValidUntil:      "",
				Comment:         "",
				ResQueue:        "pg_default",
				Createrexthttp:  false,
				Createrextgpfd:  false,
				Createwextgpfd:  false,
				Createrexthdfs:  false,
				Createwexthdfs:  false,
				TimeConstraints: nil,
			}

			for _, role := range results {
				if role.Name == "role1" {
					testutils.ExpectStructsToMatch(&expectedRole, role)
					return
				}
			}
			Fail("Role 'role1' was not found")
		})
		It("returns a role with all properties specified", func() {
			testutils.AssertQueryRuns(connection, "CREATE ROLE role1")
			defer testutils.AssertQueryRuns(connection, "DROP ROLE role1")
			testutils.AssertQueryRuns(connection, `
ALTER ROLE role1 WITH NOSUPERUSER INHERIT CREATEROLE CREATEDB LOGIN
CONNECTION LIMIT 4 PASSWORD 'swordfish' VALID UNTIL '2099-01-01 00:00:00-08'
CREATEEXTTABLE (protocol='http')
CREATEEXTTABLE (protocol='gpfdist', type='readable')
CREATEEXTTABLE (protocol='gpfdist', type='writable')
CREATEEXTTABLE (protocol='gphdfs', type='readable')
CREATEEXTTABLE (protocol='gphdfs', type='writable')`)
			testutils.AssertQueryRuns(connection, "ALTER ROLE role1 DENY BETWEEN DAY 'Sunday' TIME '1:30 PM' AND DAY 'Wednesday' TIME '14:30:00'")
			testutils.AssertQueryRuns(connection, "ALTER ROLE role1 DENY DAY 'Friday'")
			testutils.AssertQueryRuns(connection, "COMMENT ON ROLE role1 IS 'this is a role comment'")

			results := backup.GetRoles(connection)

			roleOid := testutils.OidFromRoleName(connection, "role1")
			expectedRole := backup.QueryRole{
				Oid:             roleOid,
				Name:            "role1",
				Super:           false,
				Inherit:         true,
				CreateRole:      true,
				CreateDB:        true,
				CanLogin:        true,
				ConnectionLimit: 4,
				Password:        "md5a8b2c77dfeba4705f29c094592eb3369",
				ValidUntil:      "2099-01-01 08:00:00-00",
				Comment:         "this is a role comment",
				ResQueue:        "pg_default",
				Createrexthttp:  true,
				Createrextgpfd:  true,
				Createwextgpfd:  true,
				Createrexthdfs:  true,
				Createwexthdfs:  true,
				TimeConstraints: []backup.TimeConstraint{
					{
						Oid:       0,
						StartDay:  0,
						StartTime: "13:30:00",
						EndDay:    3,
						EndTime:   "14:30:00",
					}, {
						Oid:       0,
						StartDay:  5,
						StartTime: "00:00:00",
						EndDay:    5,
						EndTime:   "24:00:00",
					},
				},
			}

			for _, role := range results {
				if role.Name == "role1" {
					testutils.ExpectStructsToMatchExcluding(&expectedRole, role, "TimeConstraints.Oid")
					return
				}
			}
			Fail("Role 'role1' was not found")
		})
	})
})
