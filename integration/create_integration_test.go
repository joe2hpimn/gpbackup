package integration

import (
	"github.com/greenplum-db/gpbackup/backup"
	"github.com/greenplum-db/gpbackup/testutils"

	"bytes"
	"regexp"

	"github.com/greenplum-db/gpbackup/utils"

	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("backup integration create statement tests", func() {
	var buffer *bytes.Buffer

	BeforeEach(func() {
		buffer = bytes.NewBuffer([]byte(""))
		testutils.SetupTestLogger()
	})
	Describe("PrintCreateSchemaStatements", func() {
		It("creates a non public schema", func() {
			schemas := []utils.Schema{{0, "test_schema", "test comment", "testrole"}}

			backup.PrintCreateSchemaStatements(buffer, schemas)

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "DROP SCHEMA test_schema")

			resultSchemas := backup.GetAllUserSchemas(connection)

			Expect(len(resultSchemas)).To(Equal(2))
			Expect(resultSchemas[0].SchemaName).To(Equal("public"))

			testutils.ExpectStructsToMatchExcluding(&schemas[0], &resultSchemas[1], "SchemaOid")

		})

		It("modifies the public schema", func() {
			schemas := []utils.Schema{{2200, "public", "test comment", "testrole"}}

			backup.PrintCreateSchemaStatements(buffer, schemas)

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "ALTER SCHEMA public OWNER TO gpadmin")
			defer testutils.AssertQueryRuns(connection, "COMMENT ON SCHEMA public IS 'standard public schema'")

			resultSchemas := backup.GetAllUserSchemas(connection)

			Expect(len(resultSchemas)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&schemas[0], &resultSchemas[0])

		})
	})

	Describe("PrintTypeStatements", func() {
		var (
			shellType         backup.TypeDefinition
			baseType          backup.TypeDefinition
			compositeTypeAtt1 backup.TypeDefinition
			compositeTypeAtt2 backup.TypeDefinition
			enumType          backup.TypeDefinition
			types             []backup.TypeDefinition
		)
		BeforeEach(func() {
			shellType = backup.TypeDefinition{Type: "p", TypeSchema: "public", TypeName: "shell_type"}
			baseType = backup.TypeDefinition{
				Type: "b", TypeSchema: "public", TypeName: "base_type", Input: "base_fn_in", Output: "base_fn_out", Receive: "-",
				Send: "-", ModIn: "-", ModOut: "-", InternalLength: 4, IsPassedByValue: true, Alignment: "i", Storage: "p",
				DefaultVal: "default", Element: "text", Delimiter: ";", Comment: "base type comment", Owner: "testrole",
			}
			compositeTypeAtt1 = backup.TypeDefinition{
				Type: "c", TypeSchema: "public", TypeName: "composite_type", Comment: "comment", Owner: "testrole",
				AttName: "att1", AttType: "text",
			}
			compositeTypeAtt2 = backup.TypeDefinition{
				Type: "c", TypeSchema: "public", TypeName: "composite_type", Comment: "comment", Owner: "testrole",
				AttName: "att2", AttType: "integer",
			}
			enumType = backup.TypeDefinition{
				Type: "e", TypeSchema: "public", TypeName: "enum_type", Comment: "comment", Owner: "testrole", EnumLabels: "'enum_labels'"}
			types = []backup.TypeDefinition{shellType, baseType, compositeTypeAtt1, compositeTypeAtt2, enumType}
		})

		It("creates shell types for base and shell types only", func() {
			backup.PrintShellTypeStatements(buffer, types)

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "DROP TYPE shell_type")
			defer testutils.AssertQueryRuns(connection, "DROP TYPE base_type")

			resultTypes := backup.GetTypeDefinitions(connection)

			Expect(len(resultTypes)).To(Equal(2))
			Expect(resultTypes[0].TypeName).To(Equal("base_type"))
			Expect(resultTypes[1].TypeName).To(Equal("shell_type"))
		})

		It("creates composite and enum types", func() {
			backup.PrintCreateCompositeAndEnumTypeStatements(buffer, types)

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "DROP TYPE composite_type")
			defer testutils.AssertQueryRuns(connection, "DROP TYPE enum_type")

			resultTypes := backup.GetTypeDefinitions(connection)

			Expect(len(resultTypes)).To(Equal(3))
			testutils.ExpectStructsToMatchIncluding(&compositeTypeAtt1, &resultTypes[0], "Type", "TypeSchema", "TypeName", "Comment", "Owner", "AttName", "AttType")
			testutils.ExpectStructsToMatchIncluding(&compositeTypeAtt2, &resultTypes[1], "Type", "TypeSchema", "TypeName", "Comment", "Owner", "AttName", "AttType")
			testutils.ExpectStructsToMatchIncluding(&enumType, &resultTypes[2], "Type", "TypeSchema", "TypeName", "Comment", "Owner", "EnumLabels")
		})

		It("creates base types", func() {
			backup.PrintCreateBaseTypeStatements(buffer, types)

			//Run queries to set up the database state so we can successfully create base types
			testutils.AssertQueryRuns(connection, "CREATE TYPE base_type")
			defer testutils.AssertQueryRuns(connection, "DROP TYPE base_type CASCADE")
			testutils.AssertQueryRuns(connection, "CREATE FUNCTION base_fn_in(cstring) RETURNS base_type AS 'boolin' LANGUAGE internal")
			testutils.AssertQueryRuns(connection, "CREATE FUNCTION base_fn_out(base_type) RETURNS cstring AS 'boolout' LANGUAGE internal")

			testutils.AssertQueryRuns(connection, buffer.String())

			resultTypes := backup.GetTypeDefinitions(connection)

			Expect(len(resultTypes)).To(Equal(1))
			testutils.ExpectStructsToMatch(&baseType, &resultTypes[0])
		})
	})

	Describe("PrintCreateViewStatements", func() {
		It("creates a view with a comment", func() {
			viewDef := backup.QueryViewDefinition{"public", "simpleview", "SELECT pg_roles.rolname FROM pg_roles;", "this is a view comment"}

			backup.PrintCreateViewStatements(buffer, []backup.QueryViewDefinition{viewDef})

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "DROP VIEW simpleview")

			resultViews := backup.GetViewDefinitions(connection)

			Expect(len(resultViews)).To(Equal(1))

			testutils.ExpectStructsToMatch(&viewDef, &resultViews[0])

		})
	})

	Describe("PrintCreateLanguageStatements", func() {
		It("creates procedural languages", func() {
			funcInfoMap := map[uint32]backup.FunctionInfo{
				1: {"pg_catalog.plpgsql_validator", "oid", true},
				2: {"pg_catalog.plpgsql_inline_handler", "internal", true},
				3: {"pg_catalog.plpgsql_call_handler", "", true},
				4: {"pg_catalog.plpython_call_handler", "", true},
				5: {"pg_catalog.plpython_inline_handler", "internal", true},
			}
			plpgsqlInfo := backup.QueryProceduralLanguage{"plpgsql", "testrole", true, true, 1, 2, 3, "", ""}
			plpythonuInfo := backup.QueryProceduralLanguage{"plpythonu", "testrole", true, false, 4, 5, 0, "", "this is a language comment"}
			procLangs := []backup.QueryProceduralLanguage{plpgsqlInfo, plpythonuInfo}

			backup.PrintCreateLanguageStatements(buffer, procLangs, funcInfoMap)

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "DROP LANGUAGE plpythonu")

			resultProcLangs := backup.GetProceduralLanguages(connection)

			Expect(len(resultProcLangs)).To(Equal(2))
			testutils.ExpectStructsToMatchExcluding(&plpgsqlInfo, &resultProcLangs[0], "Validator", "Inline", "Handler")
			testutils.ExpectStructsToMatchExcluding(&plpythonuInfo, &resultProcLangs[1], "Handler", "Inline")
		})
	})
	Describe("PrintCreateFunctionStatements", func() {
		funcMetadataMap := map[uint32]utils.ObjectMetadata{}
		It("creates a function with a simple return type", func() {
			addFunction := backup.QueryFunctionDefinition{
				SchemaName: "public", FunctionName: "add", ReturnsSet: false, FunctionBody: "SELECT $1 + $2",
				BinaryPath: "", Arguments: "integer, integer", IdentArgs: "integer, integer", ResultType: "integer",
				Volatility: "v", IsStrict: false, IsSecurityDefiner: false, Config: "", Cost: 100, NumRows: 0, DataAccess: "c",
				Language: "sql",
			}

			backup.PrintCreateFunctionStatements(buffer, []backup.QueryFunctionDefinition{addFunction}, funcMetadataMap)

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION add(integer, integer)")

			resultFunctions := backup.GetFunctionDefinitions(connection)

			Expect(len(resultFunctions)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&addFunction, &resultFunctions[0], "FunctionOid")
		})
		It("creates a function that returns a set", func() {
			appendFunction := backup.QueryFunctionDefinition{
				SchemaName: "public", FunctionName: "append", ReturnsSet: true, FunctionBody: "SELECT ($1, $2)",
				BinaryPath: "", Arguments: "integer, integer", IdentArgs: "integer, integer", ResultType: "SETOF record",
				Volatility: "s", IsStrict: true, IsSecurityDefiner: true, Config: "SET search_path TO pg_temp", Cost: 200,
				NumRows: 200, DataAccess: "m", Language: "sql",
			}

			backup.PrintCreateFunctionStatements(buffer, []backup.QueryFunctionDefinition{appendFunction}, funcMetadataMap)

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION append(integer, integer)")

			resultFunctions := backup.GetFunctionDefinitions(connection)

			Expect(len(resultFunctions)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&appendFunction, &resultFunctions[0], "FunctionOid")
		})
		It("creates a function that returns a table", func() {
			dupFunction := backup.QueryFunctionDefinition{
				SchemaName: "public", FunctionName: "dup", ReturnsSet: true, FunctionBody: "SELECT $1, CAST($1 AS text) || ' is text'",
				BinaryPath: "", Arguments: "integer", IdentArgs: "integer", ResultType: "TABLE(f1 integer, f2 text)",
				Volatility: "v", IsStrict: false, IsSecurityDefiner: false, Config: "", Cost: 100, NumRows: 1000, DataAccess: "c",
				Language: "sql",
			}

			backup.PrintCreateFunctionStatements(buffer, []backup.QueryFunctionDefinition{dupFunction}, funcMetadataMap)

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION dup(integer)")

			resultFunctions := backup.GetFunctionDefinitions(connection)

			Expect(len(resultFunctions)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&dupFunction, &resultFunctions[0], "FunctionOid")
		})
	})
	Describe("PrintCreateAggregateStatements", func() {
		It("creates an aggregate", func() {
			aggregateDef := backup.QueryAggregateDefinition{
				SchemaName: "public", AggregateName: "agg_prefunc", Arguments: "numeric, numeric",
				IdentArgs: "numeric, numeric", TransitionFunction: 1, PreliminaryFunction: 2, FinalFunction: 0,
				SortOperator: 0, TransitionDataType: "numeric", InitialValue: "0", IsOrdered: false,
				Comment: "this is an aggregate comment", Owner: "testrole",
			}
			funcInfoMap := map[uint32]backup.FunctionInfo{
				1: {QualifiedName: "public.mysfunc_accum", Arguments: "numeric, numeric, numeric"},
				2: {QualifiedName: "public.mypre_accum", Arguments: "numeric, numeric"},
			}

			backup.PrintCreateAggregateStatements(buffer, []backup.QueryAggregateDefinition{aggregateDef}, funcInfoMap)

			//Run queries to set up the database state so we can successfully create an aggregate
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
			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "DROP AGGREGATE agg_prefunc(numeric, numeric)")

			resultAggregates := backup.GetAggregateDefinitions(connection)
			Expect(len(resultAggregates)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&aggregateDef, &resultAggregates[0], "TransitionFunction", "PreliminaryFunction")
		})
	})
	Describe("PrintConstraintStatements", func() {
		var (
			testTable        utils.Relation
			tableOid         uint32
			uniqueConstraint backup.QueryConstraint
			pkConstraint     backup.QueryConstraint
			fkConstraint     backup.QueryConstraint
			checkConstraint  backup.QueryConstraint
			constraints      []string
			fkConstraints    []string
		)
		BeforeEach(func() {
			testTable = utils.BasicRelation("public", "testtable")
			uniqueConstraint = backup.QueryConstraint{ConName: "uniq2", ConType: "u", ConDef: "UNIQUE (a, b)", ConComment: "this is a constraint comment"}
			pkConstraint = backup.QueryConstraint{ConName: "pk1", ConType: "p", ConDef: "PRIMARY KEY (a, b)", ConComment: "this is a constraint comment"}
			fkConstraint = backup.QueryConstraint{ConName: "fk1", ConType: "f", ConDef: "FOREIGN KEY (b) REFERENCES constraints_other_table(b)", ConComment: ""}
			checkConstraint = backup.QueryConstraint{ConName: "check1", ConType: "c", ConDef: "CHECK (a <> 42)", ConComment: ""}
			testutils.AssertQueryRuns(connection, "CREATE TABLE public.testtable(a int, b text)")
			tableOid = testutils.OidFromRelationName(connection, "public.testtable")
		})
		AfterEach(func() {
			testutils.AssertQueryRuns(connection, "DROP TABLE testtable CASCADE")
		})
		It("creates a unique constraint", func() {
			constraints, fkConstraints = backup.ProcessConstraints(testTable, []backup.QueryConstraint{uniqueConstraint})
			backup.PrintConstraintStatements(buffer, constraints, fkConstraints)

			testutils.AssertQueryRuns(connection, buffer.String())

			resultConstraints := backup.GetConstraints(connection, tableOid)

			Expect(len(resultConstraints)).To(Equal(1))
			testutils.ExpectStructsToMatch(&uniqueConstraint, &resultConstraints[0])
		})
		It("creates a primary key constraint", func() {
			constraints, fkConstraints = backup.ProcessConstraints(testTable, []backup.QueryConstraint{pkConstraint})
			backup.PrintConstraintStatements(buffer, constraints, fkConstraints)

			testutils.AssertQueryRuns(connection, buffer.String())

			resultConstraints := backup.GetConstraints(connection, tableOid)

			Expect(len(resultConstraints)).To(Equal(1))
			testutils.ExpectStructsToMatch(&pkConstraint, &resultConstraints[0])
		})
		It("creates a fk constraint", func() {
			constraints, fkConstraints = backup.ProcessConstraints(testTable, []backup.QueryConstraint{fkConstraint})
			backup.PrintConstraintStatements(buffer, constraints, fkConstraints)

			testutils.AssertQueryRuns(connection, "CREATE TABLE constraints_other_table(b text PRIMARY KEY)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE constraints_other_table CASCADE")
			testutils.AssertQueryRuns(connection, buffer.String())

			resultConstraints := backup.GetConstraints(connection, tableOid)

			Expect(len(resultConstraints)).To(Equal(1))
			testutils.ExpectStructsToMatch(&fkConstraint, &resultConstraints[0])
		})
		It("creates a check constraint", func() {
			constraints, fkConstraints = backup.ProcessConstraints(testTable, []backup.QueryConstraint{checkConstraint})
			backup.PrintConstraintStatements(buffer, constraints, fkConstraints)

			testutils.AssertQueryRuns(connection, buffer.String())

			resultConstraints := backup.GetConstraints(connection, tableOid)

			Expect(len(resultConstraints)).To(Equal(1))
			testutils.ExpectStructsToMatch(&checkConstraint, &resultConstraints[0])
		})
		It("creates multiple constraints on one table", func() {
			constraints, fkConstraints = backup.ProcessConstraints(testTable, []backup.QueryConstraint{checkConstraint, pkConstraint, uniqueConstraint, fkConstraint})
			backup.PrintConstraintStatements(buffer, constraints, fkConstraints)

			testutils.AssertQueryRuns(connection, "CREATE TABLE constraints_other_table(b text PRIMARY KEY)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE constraints_other_table CASCADE")
			testutils.AssertQueryRuns(connection, buffer.String())

			resultConstraints := backup.GetConstraints(connection, tableOid)

			Expect(len(resultConstraints)).To(Equal(4))
			testutils.ExpectStructsToMatch(&checkConstraint, &resultConstraints[0])
			testutils.ExpectStructsToMatch(&pkConstraint, &resultConstraints[1])
			testutils.ExpectStructsToMatch(&uniqueConstraint, &resultConstraints[2])
			testutils.ExpectStructsToMatch(&fkConstraint, &resultConstraints[3])
		})
	})
	Describe("PrintCreateSequenceStatements", func() {
		var (
			columnOwnerMap      map[string]string
			sequence            utils.Relation
			sequenceDef         backup.Sequence
			sequenceMetadataMap map[uint32]utils.ObjectMetadata
		)
		BeforeEach(func() {
			sequence = utils.Relation{0, 1, "public", "my_sequence"}
			sequenceDef = backup.Sequence{Relation: sequence}
			columnOwnerMap = map[string]string{}
			sequenceMetadataMap = map[uint32]utils.ObjectMetadata{}
		})
		It("creates a basic sequence", func() {
			sequenceDef.QuerySequenceDefinition = backup.QuerySequenceDefinition{Name: "my_sequence", LastVal: 1, Increment: 1, MaxVal: 9223372036854775807, MinVal: 1, CacheVal: 1}
			backup.PrintCreateSequenceStatements(buffer, []backup.Sequence{sequenceDef}, columnOwnerMap, sequenceMetadataMap)

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "DROP SEQUENCE my_sequence")

			resultSequences := backup.GetAllSequences(connection)

			Expect(len(resultSequences)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&sequence, &resultSequences[0].Relation, "SchemaOid", "RelationOid")
			testutils.ExpectStructsToMatch(&sequenceDef.QuerySequenceDefinition, &resultSequences[0].QuerySequenceDefinition)
		})
		It("creates a complex sequence", func() {
			sequenceDef.QuerySequenceDefinition = backup.QuerySequenceDefinition{Name: "my_sequence", LastVal: 105, Increment: 5, MaxVal: 1000, MinVal: 20, CacheVal: 1, LogCnt: 0, IsCycled: false, IsCalled: true}
			backup.PrintCreateSequenceStatements(buffer, []backup.Sequence{sequenceDef}, columnOwnerMap, sequenceMetadataMap)

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "DROP SEQUENCE my_sequence")

			resultSequences := backup.GetAllSequences(connection)

			Expect(len(resultSequences)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&sequence, &resultSequences[0].Relation, "SchemaOid", "RelationOid")
			testutils.ExpectStructsToMatch(&sequenceDef.QuerySequenceDefinition, &resultSequences[0].QuerySequenceDefinition)
		})
		It("creates a sequence owned by a table column", func() {
			sequenceDef.QuerySequenceDefinition = backup.QuerySequenceDefinition{Name: "my_sequence",
				LastVal: 1, Increment: 1, MaxVal: 9223372036854775807, MinVal: 1, CacheVal: 1}
			columnOwnerMap["public.my_sequence"] = "sequence_table.a"
			backup.PrintCreateSequenceStatements(buffer, []backup.Sequence{sequenceDef}, columnOwnerMap, sequenceMetadataMap)

			//Create table that sequence can be owned by
			testutils.AssertQueryRuns(connection, "CREATE TABLE sequence_table(a int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE sequence_table")

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "DROP SEQUENCE my_sequence")

			resultSequences := backup.GetAllSequences(connection)

			Expect(len(resultSequences)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&sequence, &resultSequences[0].Relation, "SchemaOid", "RelationOid")
			testutils.ExpectStructsToMatch(&sequenceDef.QuerySequenceDefinition, &resultSequences[0].QuerySequenceDefinition)
		})
		It("creates a sequence with privileges, owner, and comment", func() {
			sequenceDef.QuerySequenceDefinition = backup.QuerySequenceDefinition{Name: "my_sequence", LastVal: 1, Increment: 1, MaxVal: 9223372036854775807, MinVal: 1, CacheVal: 1}
			sequenceMetadata := utils.ObjectMetadata{[]utils.ACL{utils.DefaultACLWithout("testrole", "SEQUENCE", "UPDATE")}, "testrole", "This is a sequence comment."}
			sequenceMetadataMap[1] = sequenceMetadata
			backup.PrintCreateSequenceStatements(buffer, []backup.Sequence{sequenceDef}, columnOwnerMap, sequenceMetadataMap)

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "DROP SEQUENCE my_sequence")

			resultSequences := backup.GetAllSequences(connection)

			Expect(len(resultSequences)).To(Equal(1))
			resultMetadataMap := backup.GetMetadataForObjectType(connection, "relnamespace", "relacl", "relowner", "pg_class")
			oid := testutils.OidFromRelationName(connection, "public.my_sequence")
			resultMetadata := resultMetadataMap[oid]
			testutils.ExpectStructsToMatchExcluding(&sequence, &resultSequences[0].Relation, "SchemaOid", "RelationOid")
			testutils.ExpectStructsToMatch(&sequenceDef.QuerySequenceDefinition, &resultSequences[0].QuerySequenceDefinition)
			testutils.ExpectStructsToMatch(&sequenceMetadata, &resultMetadata)
		})
	})
	Describe("PrintSessionGUCs", func() {
		It("prints the default session GUCs", func() {
			gucs := backup.QuerySessionGUCs{ClientEncoding: "UTF8", StdConformingStrings: "on", DefaultWithOids: "off"}

			backup.PrintSessionGUCs(buffer, gucs)

			//We just want to check that these queries run successfully, no setup required
			testutils.AssertQueryRuns(connection, buffer.String())
		})
	})
	Describe("PrintPostdataCreateStatements", func() {
		It("creates all indexes for all tables", func() {
			testTable := utils.BasicRelation("public", "index_table")
			btree := "\n\nCREATE INDEX simple_table_idx1 ON index_table USING btree (a);"
			bitmap := "\n\nCREATE INDEX simple_table_idx2 ON index_table USING bitmap (b);\nCOMMENT ON INDEX simple_table_idx2 IS 'this is a index comment';"

			backup.PrintPostdataCreateStatements(buffer, []string{btree, bitmap})

			//Create table whose columns we can index
			testutils.AssertQueryRuns(connection, "CREATE TABLE index_table(a int, b text)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE index_table")
			testTable.RelationOid = testutils.OidFromRelationName(connection, "public.index_table")

			testutils.AssertQueryRuns(connection, buffer.String())
			resultIndexes := backup.GetIndexesForAllTables(connection, []utils.Relation{testTable})
			Expect(len(resultIndexes)).To(Equal(2))
			Expect(resultIndexes[0]).To(Equal(btree))
			Expect(resultIndexes[1]).To(Equal(bitmap))
		})
		It("creates all rules for all tables", func() {
			insert := "\n\nCREATE RULE double_insert AS ON INSERT TO rule_table1 DO INSERT INTO rule_table2 DEFAULT VALUES;"
			update := "\n\nCREATE RULE update_notify AS ON UPDATE TO rule_table1 DO NOTIFY rule_table1;\nCOMMENT ON RULE update_notify ON public.rule_table1 IS 'This is a rule comment.';"

			backup.PrintPostdataCreateStatements(buffer, []string{insert, update})

			testutils.AssertQueryRuns(connection, "CREATE TABLE rule_table1(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE rule_table1")
			testutils.AssertQueryRuns(connection, "CREATE TABLE rule_table2(j int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE rule_table2")
			defer testutils.AssertQueryRuns(connection, "DROP RULE double_insert ON rule_table1")
			defer testutils.AssertQueryRuns(connection, "DROP RULE update_notify ON rule_table1")

			testutils.AssertQueryRuns(connection, buffer.String())
			resultRules := backup.GetRuleDefinitions(connection)
			Expect(len(resultRules)).To(Equal(2))
			Expect(resultRules[0]).To(Equal(insert))
			Expect(resultRules[1]).To(Equal(update))
		})
		It("creates all triggers for all tables", func() {
			sync1 := "\n\nCREATE TRIGGER sync_trigger_table1 AFTER INSERT OR DELETE OR UPDATE ON trigger_table1 FOR EACH STATEMENT EXECUTE PROCEDURE flatfile_update_trigger();"
			sync2 := "\n\nCREATE TRIGGER sync_trigger_table2 AFTER INSERT OR DELETE OR UPDATE ON trigger_table2 FOR EACH STATEMENT EXECUTE PROCEDURE flatfile_update_trigger();\nCOMMENT ON TRIGGER sync_trigger_table2 ON public.trigger_table2 IS 'This is a trigger comment.';"

			backup.PrintPostdataCreateStatements(buffer, []string{sync1, sync2})

			testutils.AssertQueryRuns(connection, "CREATE TABLE trigger_table1(i int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE trigger_table1")
			testutils.AssertQueryRuns(connection, "CREATE TABLE trigger_table2(j int)")
			defer testutils.AssertQueryRuns(connection, "DROP TABLE trigger_table2")
			defer testutils.AssertQueryRuns(connection, "DROP TRIGGER sync_trigger_table1 ON trigger_table1")
			defer testutils.AssertQueryRuns(connection, "DROP TRIGGER sync_trigger_table2 ON trigger_table2")

			testutils.AssertQueryRuns(connection, buffer.String())
			resultTriggers := backup.GetTriggerDefinitions(connection)
			Expect(len(resultTriggers)).To(Equal(2))
			Expect(resultTriggers[0]).To(Equal(sync1))
			Expect(resultTriggers[1]).To(Equal(sync2))
		})
	})
	Describe("PrintCreateCastStatements", func() {
		It("creates a cast", func() {
			castDef := backup.QueryCastDefinition{SourceType: "text", TargetType: "integer", FunctionSchema: "public",
				FunctionName: "casttoint", FunctionArgs: "text", CastContext: "a", Comment: ""}

			testutils.AssertQueryRuns(connection, "CREATE FUNCTION casttoint(text) RETURNS integer STRICT IMMUTABLE LANGUAGE SQL AS 'SELECT cast($1 as integer);'")
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION casttoint(text)")

			backup.PrintCreateCastStatements(buffer, []backup.QueryCastDefinition{castDef})
			defer testutils.AssertQueryRuns(connection, "DROP CAST (text AS integer)")

			testutils.AssertQueryRuns(connection, buffer.String())

			resultCasts := backup.GetCastDefinitions(connection)
			Expect(len(resultCasts)).To(Equal(1))
			testutils.ExpectStructsToMatch(&castDef, &resultCasts[0])
		})
	})
	Describe("PrintRegularTableCreateStatement", func() {
		var (
			extTableEmpty backup.ExternalTableDefinition
			testTable     utils.Relation
			tableDef      backup.TableDefinition
			/*
			 * We need to construct partitionDef and partTemplateDef piecemeal like this,
			 * or go fmt will remove the trailing whitespace and prevent literal comparison.
			 */
			partitionDef = `PARTITION BY LIST(gender) ` + `
          (
          PARTITION girls VALUES('F') WITH (tablename='rank_1_prt_girls', appendonly=false ), ` + `
          PARTITION boys VALUES('M') WITH (tablename='rank_1_prt_boys', appendonly=false ), ` + `
          DEFAULT PARTITION other  WITH (tablename='rank_1_prt_other', appendonly=false )
          )`
			subpartitionDef = `PARTITION BY LIST(gender)
          SUBPARTITION BY LIST(region) ` + `
          (
          PARTITION girls VALUES('F') WITH (tablename='rank_1_prt_girls', appendonly=false ) ` + `
                  (
                  SUBPARTITION usa VALUES('usa') WITH (tablename='rank_1_prt_girls_2_prt_usa', appendonly=false ), ` + `
                  SUBPARTITION asia VALUES('asia') WITH (tablename='rank_1_prt_girls_2_prt_asia', appendonly=false ), ` + `
                  SUBPARTITION europe VALUES('europe') WITH (tablename='rank_1_prt_girls_2_prt_europe', appendonly=false ), ` + `
                  DEFAULT SUBPARTITION other_regions  WITH (tablename='rank_1_prt_girls_2_prt_other_regions', appendonly=false )
                  ), ` + `
          PARTITION boys VALUES('M') WITH (tablename='rank_1_prt_boys', appendonly=false ) ` + `
                  (
                  SUBPARTITION usa VALUES('usa') WITH (tablename='rank_1_prt_boys_2_prt_usa', appendonly=false ), ` + `
                  SUBPARTITION asia VALUES('asia') WITH (tablename='rank_1_prt_boys_2_prt_asia', appendonly=false ), ` + `
                  SUBPARTITION europe VALUES('europe') WITH (tablename='rank_1_prt_boys_2_prt_europe', appendonly=false ), ` + `
                  DEFAULT SUBPARTITION other_regions  WITH (tablename='rank_1_prt_boys_2_prt_other_regions', appendonly=false )
                  ), ` + `
          DEFAULT PARTITION other  WITH (tablename='rank_1_prt_other', appendonly=false ) ` + `
                  (
                  SUBPARTITION usa VALUES('usa') WITH (tablename='rank_1_prt_other_2_prt_usa', appendonly=false ), ` + `
                  SUBPARTITION asia VALUES('asia') WITH (tablename='rank_1_prt_other_2_prt_asia', appendonly=false ), ` + `
                  SUBPARTITION europe VALUES('europe') WITH (tablename='rank_1_prt_other_2_prt_europe', appendonly=false ), ` + `
                  DEFAULT SUBPARTITION other_regions  WITH (tablename='rank_1_prt_other_2_prt_other_regions', appendonly=false )
                  )
          )`
			partTemplateDef = `ALTER TABLE test_table ` + `
SET SUBPARTITION TEMPLATE  ` + `
          (
          SUBPARTITION usa VALUES('usa') WITH (tablename='test_table'), ` + `
          SUBPARTITION asia VALUES('asia') WITH (tablename='test_table'), ` + `
          SUBPARTITION europe VALUES('europe') WITH (tablename='test_table'), ` + `
          DEFAULT SUBPARTITION other_regions  WITH (tablename='test_table')
          )
`
		)
		BeforeEach(func() {
			extTableEmpty = backup.ExternalTableDefinition{-2, -2, "", "ALL_SEGMENTS", "t", "", "", "", 0, "", "", "UTF-8", false}
			testTable = utils.BasicRelation("public", "test_table")
			tableDef = backup.TableDefinition{DistPolicy: "DISTRIBUTED RANDOMLY", ExtTableDef: extTableEmpty}
		})
		AfterEach(func() {
			testutils.AssertQueryRuns(connection, "DROP TABLE public.test_table")
		})
		It("creates a table with no attributes", func() {
			tableDef.ColumnDefs = []backup.ColumnDefinition{}

			backup.PrintRegularTableCreateStatement(buffer, testTable, tableDef)

			testutils.AssertQueryRuns(connection, buffer.String())
			testTable.RelationOid = testutils.OidFromRelationName(connection, "public.test_table")
			resultTableDef := backup.ConstructDefinitionsForTable(connection, testTable, false)
			testutils.ExpectStructsToMatchExcluding(&tableDef, &resultTableDef, "ExtTableDef")
		})
		It("creates a basic heap table", func() {
			rowOne := backup.ColumnDefinition{1, "i", false, false, false, "integer", "", "", ""}
			rowTwo := backup.ColumnDefinition{2, "j", false, false, false, "character varying(20)", "", "", ""}
			tableDef.ColumnDefs = []backup.ColumnDefinition{rowOne, rowTwo}

			backup.PrintRegularTableCreateStatement(buffer, testTable, tableDef)

			testutils.AssertQueryRuns(connection, buffer.String())
			testTable.RelationOid = testutils.OidFromRelationName(connection, "public.test_table")
			resultTableDef := backup.ConstructDefinitionsForTable(connection, testTable, false)
			testutils.ExpectStructsToMatchExcluding(&tableDef, &resultTableDef, "ExtTableDef")
		})
		It("creates a complex heap table", func() {
			rowOneDefault := backup.ColumnDefinition{1, "i", false, true, false, "integer", "", "", "42"}
			rowNotNullDefault := backup.ColumnDefinition{2, "j", true, true, false, "character varying(20)", "", "", "'bar'::text"}
			tableDef.DistPolicy = "DISTRIBUTED BY (i, j)"
			tableDef.ColumnDefs = []backup.ColumnDefinition{rowOneDefault, rowNotNullDefault}

			backup.PrintRegularTableCreateStatement(buffer, testTable, tableDef)

			testutils.AssertQueryRuns(connection, buffer.String())
			testTable.RelationOid = testutils.OidFromRelationName(connection, "public.test_table")
			resultTableDef := backup.ConstructDefinitionsForTable(connection, testTable, false)
			testutils.ExpectStructsToMatchExcluding(&tableDef, &resultTableDef, "ExtTableDef")
		})
		It("creates a basic append-optimized column-oriented table", func() {
			rowOne := backup.ColumnDefinition{1, "i", false, false, false, "integer", "compresstype=zlib,blocksize=32768,compresslevel=1", "", ""}
			rowTwo := backup.ColumnDefinition{2, "j", false, false, false, "character varying(20)", "compresstype=zlib,blocksize=32768,compresslevel=1", "", ""}
			tableDef.StorageOpts = "appendonly=true, orientation=column, fillfactor=42, compresstype=zlib, blocksize=32768, compresslevel=1"
			tableDef.ColumnDefs = []backup.ColumnDefinition{rowOne, rowTwo}

			backup.PrintRegularTableCreateStatement(buffer, testTable, tableDef)

			testutils.AssertQueryRuns(connection, buffer.String())
			testTable.RelationOid = testutils.OidFromRelationName(connection, "public.test_table")
			resultTableDef := backup.ConstructDefinitionsForTable(connection, testTable, false)
			testutils.ExpectStructsToMatchExcluding(&tableDef, &resultTableDef, "ExtTableDef")
		})
		It("creates a one-level partition table", func() {
			rowOne := backup.ColumnDefinition{1, "region", false, false, false, "text", "", "", ""}
			rowTwo := backup.ColumnDefinition{2, "gender", false, false, false, "text", "", "", ""}
			tableDef.PartDef = partitionDef
			tableDef.ColumnDefs = []backup.ColumnDefinition{rowOne, rowTwo}

			backup.PrintRegularTableCreateStatement(buffer, testTable, tableDef)

			testutils.AssertQueryRuns(connection, buffer.String())
			testTable.RelationOid = testutils.OidFromRelationName(connection, "public.test_table")
			resultTableDef := backup.ConstructDefinitionsForTable(connection, testTable, false)
			testutils.ExpectStructsToMatchExcluding(&tableDef, &resultTableDef, "ExtTableDef")
		})
		It("creates a two-level partition table", func() {
			rowOne := backup.ColumnDefinition{1, "region", false, false, false, "text", "", "", ""}
			rowTwo := backup.ColumnDefinition{2, "gender", false, false, false, "text", "", "", ""}
			tableDef.PartDef = subpartitionDef
			tableDef.PartTemplateDef = partTemplateDef
			tableDef.ColumnDefs = []backup.ColumnDefinition{rowOne, rowTwo}

			backup.PrintRegularTableCreateStatement(buffer, testTable, tableDef)

			testutils.AssertQueryRuns(connection, buffer.String())
			testTable.RelationOid = testutils.OidFromRelationName(connection, "public.test_table")
			resultTableDef := backup.ConstructDefinitionsForTable(connection, testTable, false)
			testutils.ExpectStructsToMatchExcluding(&tableDef, &resultTableDef, "ExtTableDef")
		})
	})
	Describe("PrintPostCreateTableStatements", func() {
		var (
			extTableEmpty = backup.ExternalTableDefinition{-2, -2, "", "ALL_SEGMENTS", "t", "", "", "", 0, "", "", "UTF-8", false}
			testTable     = utils.BasicRelation("public", "test_table")
			tableRow      = backup.ColumnDefinition{1, "i", false, false, false, "integer", "", "", ""}
			tableDef      = backup.TableDefinition{DistPolicy: "DISTRIBUTED BY (i)", ColumnDefs: []backup.ColumnDefinition{tableRow}, ExtTableDef: extTableEmpty}
			tableMetadata utils.ObjectMetadata
		)
		BeforeEach(func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLE test_table(i int)")
			tableMetadata = utils.ObjectMetadata{Privileges: []utils.ACL{}}
		})
		AfterEach(func() {
			testutils.AssertQueryRuns(connection, "DROP TABLE test_table")
		})
		It("prints only owner for a table with no comment or column comments", func() {
			tableMetadata.Owner = "testrole"
			backup.PrintPostCreateTableStatements(buffer, testTable, tableDef, tableMetadata)

			testutils.AssertQueryRuns(connection, buffer.String())
			testTable.RelationOid = testutils.OidFromRelationName(connection, "public.test_table")
			resultMetadata := backup.GetMetadataForObjectType(connection, "relnamespace", "relacl", "relowner", "pg_class")
			resultTableMetadata := resultMetadata[testTable.RelationOid]
			testutils.ExpectStructsToMatch(&tableMetadata, &resultTableMetadata)
			resultTableDef := backup.ConstructDefinitionsForTable(connection, testTable, false)
			testutils.ExpectStructsToMatchExcluding(&tableDef, &resultTableDef, "ExtTableDef")
		})
		It("prints table comment, table owner, and column comments for a table with all three", func() {
			tableMetadata.Owner = "testrole"
			tableMetadata.Comment = "This is a table comment."
			tableDef.ColumnDefs[0].Comment = "This is a column comment."
			backup.PrintPostCreateTableStatements(buffer, testTable, tableDef, tableMetadata)

			testutils.AssertQueryRuns(connection, buffer.String())
			testTable.RelationOid = testutils.OidFromRelationName(connection, "public.test_table")
			resultTableDef := backup.ConstructDefinitionsForTable(connection, testTable, false)
			testutils.ExpectStructsToMatchExcluding(&tableDef, &resultTableDef, "ExtTableDef")
			resultMetadata := backup.GetMetadataForObjectType(connection, "relnamespace", "relacl", "relowner", "pg_class")
			resultTableMetadata := resultMetadata[testTable.RelationOid]
			testutils.ExpectStructsToMatch(&tableMetadata, &resultTableMetadata)
		})
	})
	Describe("PrintExternalTableCreateStatement", func() {
		var (
			extTable  backup.ExternalTableDefinition
			testTable utils.Relation
			tableDef  backup.TableDefinition
		)
		BeforeEach(func() {
			extTable = backup.ExternalTableDefinition{
				0, backup.FILE, "file://tmp/ext_table_file", "ALL_SEGMENTS",
				"t", "delimiter '	' null '\\N' escape '\\'", "", "",
				0, "", "", "UTF8", false}
			testTable = utils.BasicRelation("public", "test_table")
			tableDef = backup.TableDefinition{IsExternal: true}
			os.Create("/tmp/ext_table_file")
		})
		AfterEach(func() {
			os.Remove("/tmp/ext_table_file")
			testutils.AssertQueryRuns(connection, "DROP EXTERNAL TABLE test_table")
		})
		It("creates a READABLE EXTERNAL table", func() {
			extTable.Type = backup.READABLE
			extTable.Writable = false
			tableDef.ExtTableDef = extTable

			backup.PrintExternalTableCreateStatement(buffer, testTable, tableDef)

			testutils.AssertQueryRuns(connection, buffer.String())

			testTable.RelationOid = testutils.OidFromRelationName(connection, "public.test_table")
			resultTableDef := backup.GetExternalTableDefinition(connection, testTable.RelationOid)
			resultTableDef.Type, resultTableDef.Protocol = backup.DetermineExternalTableCharacteristics(resultTableDef)

			testutils.ExpectStructsToMatch(&extTable, &resultTableDef)
		})
		It("creates a WRITABLE EXTERNAL table", func() {
			extTable.Type = backup.WRITABLE
			extTable.Writable = true
			extTable.Location = "gpfdist://outputhost:8081/data1.out"
			extTable.Protocol = backup.GPFDIST
			tableDef.ExtTableDef = extTable

			backup.PrintExternalTableCreateStatement(buffer, testTable, tableDef)

			testutils.AssertQueryRuns(connection, buffer.String())

			testTable.RelationOid = testutils.OidFromRelationName(connection, "public.test_table")
			resultTableDef := backup.GetExternalTableDefinition(connection, testTable.RelationOid)
			resultTableDef.Type, resultTableDef.Protocol = backup.DetermineExternalTableCharacteristics(resultTableDef)

			testutils.ExpectStructsToMatch(&extTable, &resultTableDef)
		})
	})
	Describe("PrintCreateExternalProtocolStatements", func() {
		funcInfoMap := map[uint32]backup.FunctionInfo{
			1: {"public.write_to_s3", "", false},
			2: {"public.read_from_s3", "", false},
		}
		protocolReadOnly := backup.QueryExtProtocol{"s3_read", "testrole", true, 2, 0, 0, ""}
		protocolWriteOnly := backup.QueryExtProtocol{"s3_write", "testrole", false, 0, 1, 0, ""}
		protocolReadWrite := backup.QueryExtProtocol{"s3_read_write", "testrole", false, 2, 1, 0, ""}
		It("creates a trusted protocol with a read function", func() {
			externalProtocols := []backup.QueryExtProtocol{protocolReadOnly}

			backup.PrintCreateExternalProtocolStatements(buffer, externalProtocols, funcInfoMap)

			testutils.AssertQueryRuns(connection, "CREATE OR REPLACE FUNCTION read_from_s3() RETURNS integer AS '$libdir/gps3ext.so', 's3_import' LANGUAGE C STABLE;")
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION read_from_s3()")

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "DROP PROTOCOL s3_read")

			resultExternalProtocols := backup.GetExternalProtocols(connection)

			Expect(len(resultExternalProtocols)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&protocolReadOnly, &resultExternalProtocols[0], "ReadFunction")
		})
		It("creates a protocol with a write function", func() {
			externalProtocols := []backup.QueryExtProtocol{protocolWriteOnly}

			backup.PrintCreateExternalProtocolStatements(buffer, externalProtocols, funcInfoMap)

			testutils.AssertQueryRuns(connection, "CREATE OR REPLACE FUNCTION write_to_s3() RETURNS integer AS '$libdir/gps3ext.so', 's3_export' LANGUAGE C STABLE;")
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION write_to_s3()")

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "DROP PROTOCOL s3_write")

			resultExternalProtocols := backup.GetExternalProtocols(connection)

			Expect(len(resultExternalProtocols)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&protocolWriteOnly, &resultExternalProtocols[0], "WriteFunction")
		})
		It("creates a protocol with a read and write function", func() {
			externalProtocols := []backup.QueryExtProtocol{protocolReadWrite}

			backup.PrintCreateExternalProtocolStatements(buffer, externalProtocols, funcInfoMap)

			testutils.AssertQueryRuns(connection, "CREATE OR REPLACE FUNCTION read_from_s3() RETURNS integer AS '$libdir/gps3ext.so', 's3_import' LANGUAGE C STABLE;")
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION read_from_s3()")

			testutils.AssertQueryRuns(connection, "CREATE OR REPLACE FUNCTION write_to_s3() RETURNS integer AS '$libdir/gps3ext.so', 's3_export' LANGUAGE C STABLE;")
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION write_to_s3()")

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, "DROP PROTOCOL s3_read_write")

			resultExternalProtocols := backup.GetExternalProtocols(connection)

			Expect(len(resultExternalProtocols)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&protocolReadWrite, &resultExternalProtocols[0], "ReadFunction", "WriteFunction")
		})
	})
	Describe("PrintCreateResourceQueueStatements", func() {
		It("creates a basic resource queue with a comment", func() {
			basicQueue := backup.QueryResourceQueue{"basicQueue", -1, "32.80", false, "0.00", "medium", "-1", "this is a resource queue comment"}

			backup.PrintCreateResourceQueueStatements(buffer, []backup.QueryResourceQueue{basicQueue})

			// CREATE RESOURCE QUEUE statements can not be part of a multi-command statement, so
			// feed the CREATE RESOURCE QUEUE and COMMENT ON statements separately.
			hunks := regexp.MustCompile(";\n\n").Split(buffer.String(), 2)
			testutils.AssertQueryRuns(connection, hunks[0])
			defer testutils.AssertQueryRuns(connection, `DROP RESOURCE QUEUE "basicQueue"`)
			testutils.AssertQueryRuns(connection, hunks[1])

			resultResourceQueues := backup.GetResourceQueues(connection)

			for _, resultQueue := range resultResourceQueues {
				if resultQueue.Name == "basicQueue" {
					testutils.ExpectStructsToMatch(&basicQueue, &resultQueue)
					return
				}
			}
		})
		It("creates a resource queue with all attributes", func() {
			everythingQueue := backup.QueryResourceQueue{"everythingQueue", 7, "32.80", true, "22.80", "low", "2GB", ""}

			backup.PrintCreateResourceQueueStatements(buffer, []backup.QueryResourceQueue{everythingQueue})

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, `DROP RESOURCE QUEUE "everythingQueue"`)

			resultResourceQueues := backup.GetResourceQueues(connection)

			for _, resultQueue := range resultResourceQueues {
				if resultQueue.Name == "everythingQueue" {
					testutils.ExpectStructsToMatch(&everythingQueue, &resultQueue)
					return
				}
			}
			Fail("didn't find everythingQueue :(")
		})
	})
	Describe("PrintCreateRoleStatements", func() {
		It("creates a basic role ", func() {
			role1 := backup.QueryRole{
				Oid:             0,
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

			backup.PrintCreateRoleStatements(buffer, []backup.QueryRole{role1})

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, `DROP ROLE "role1"`)
			role1.Oid = testutils.OidFromRoleName(connection, "role1")

			resultRoles := backup.GetRoles(connection)
			for _, role := range resultRoles {
				if role.Name == "role1" {
					testutils.ExpectStructsToMatch(&role1, role)
					return
				}
			}
			Fail("Role 'role1' was not found")
		})
		It("creates a role with all attributes", func() {
			role1 := backup.QueryRole{
				Oid:             0,
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

			backup.PrintCreateRoleStatements(buffer, []backup.QueryRole{role1})

			testutils.AssertQueryRuns(connection, buffer.String())
			defer testutils.AssertQueryRuns(connection, `DROP ROLE "role1"`)
			role1.Oid = testutils.OidFromRoleName(connection, "role1")

			resultRoles := backup.GetRoles(connection)
			for _, role := range resultRoles {
				if role.Name == "role1" {
					testutils.ExpectStructsToMatchExcluding(&role1, role, "TimeConstraints.Oid")
					return
				}
			}
			Fail("Role 'role1' was not found")
		})
	})
})
