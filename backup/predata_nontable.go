package backup

/*
 * This file contains structs and functions related to dumping non-table-related
 * metadata on the master that needs to be restored before data is restored, such
 * as sequences and check constraints.
 */

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/greenplum-db/gpbackup/utils"
)

type Sequence struct {
	utils.Relation
	QuerySequenceDefinition
}

/*
 * Functions to print to the predata file
 */

func PrintObjectMetadata(file io.Writer, obj utils.ObjectMetadata, objectName string, objectType string) {
	utils.MustPrintf(file, obj.GetCommentStatement(objectName, objectType))
	utils.MustPrintf(file, obj.GetOwnerStatement(objectName, objectType))
	utils.MustPrintf(file, obj.GetPrivilegesStatements(objectName, objectType))
}

/*
 * This function calls per-table functions to get constraints related to each
 * table, then consolidates them in two slices holding all constraints for all
 * tables.  Two slices are needed because FOREIGN KEY constraints must be dumped
 * after PRIMARY KEY constraints, so they're separated out to be handled last.
 */
func ConstructConstraintsForAllTables(connection *utils.DBConn, tables []utils.Relation) ([]string, []string) {
	allConstraints := make([]string, 0)
	allFkConstraints := make([]string, 0)
	for _, table := range tables {
		constraintList := GetConstraints(connection, table.RelationOid)
		tableConstraints, tableFkConstraints := ProcessConstraints(table, constraintList)
		allConstraints = append(allConstraints, tableConstraints...)
		allFkConstraints = append(allFkConstraints, tableFkConstraints...)
	}
	return allConstraints, allFkConstraints
}

/*
 * There's no built-in function to generate constraint definitions like there is for other types of
 * metadata, so this function constructs them.
 */
func ProcessConstraints(table utils.Relation, constraints []QueryConstraint) ([]string, []string) {
	alterStr := fmt.Sprintf("\n\nALTER TABLE ONLY %s ADD CONSTRAINT %s %s;", table.ToString(), "%s", "%s")
	commentStr := fmt.Sprintf("\n\nCOMMENT ON CONSTRAINT %s ON %s IS '%s';", "%s", table.ToString(), "%s")
	cons := make([]string, 0)
	fkCons := make([]string, 0)
	for _, constraint := range constraints {
		conStr := fmt.Sprintf(alterStr, utils.QuoteIdent(constraint.ConName), constraint.ConDef)
		if constraint.ConComment != "" {
			conStr += fmt.Sprintf(commentStr, utils.QuoteIdent(constraint.ConName), constraint.ConComment)
		}
		if constraint.ConType == "f" {
			fkCons = append(fkCons, conStr)
		} else {
			cons = append(cons, conStr)
		}
	}
	return cons, fkCons
}

func PrintConstraintStatements(predataFile io.Writer, constraints []string, fkConstraints []string) {
	sort.Strings(constraints)
	sort.Strings(fkConstraints)
	for _, constraint := range constraints {
		utils.MustPrintln(predataFile, constraint)
	}
	for _, constraint := range fkConstraints {
		utils.MustPrintln(predataFile, constraint)
	}
}

func PrintCreateSchemaStatements(predataFile io.Writer, schemas []utils.Schema, schemaMetadata utils.MetadataMap) {
	for _, schema := range schemas {
		utils.MustPrintln(predataFile)
		if schema.SchemaName != "public" {
			utils.MustPrintf(predataFile, "\nCREATE SCHEMA %s;", schema.ToString())
		}
		PrintObjectMetadata(predataFile, schemaMetadata[schema.SchemaOid], schema.ToString(), "SCHEMA")
	}
}

func GetAllSequences(connection *utils.DBConn) []Sequence {
	sequenceRelations := GetAllSequenceRelations(connection)
	sequences := make([]Sequence, 0)
	for _, seqRelation := range sequenceRelations {
		seqDef := GetSequenceDefinition(connection, seqRelation.ToString())
		sequence := Sequence{seqRelation, seqDef}
		sequences = append(sequences, sequence)
	}
	return sequences
}

/*
 * This function is largely derived from the dumpSequence() function in pg_dump.c.  The values of
 * minVal and maxVal come from SEQ_MINVALUE and SEQ_MAXVALUE, defined in include/commands/sequence.h.
 */
func PrintCreateSequenceStatements(predataFile io.Writer, sequences []Sequence, sequenceColumnOwners map[string]string, sequenceMetadata utils.MetadataMap) {
	maxVal := int64(9223372036854775807)
	minVal := int64(-9223372036854775807)
	for _, sequence := range sequences {
		seqFQN := sequence.ToString()
		utils.MustPrintln(predataFile, "\n\nCREATE SEQUENCE", seqFQN)
		if !sequence.IsCalled {
			utils.MustPrintln(predataFile, "\tSTART WITH", sequence.LastVal)
		}
		utils.MustPrintln(predataFile, "\tINCREMENT BY", sequence.Increment)

		if !((sequence.MaxVal == maxVal && sequence.Increment > 0) || (sequence.MaxVal == -1 && sequence.Increment < 0)) {
			utils.MustPrintln(predataFile, "\tMAXVALUE", sequence.MaxVal)
		} else {
			utils.MustPrintln(predataFile, "\tNO MAXVALUE")
		}
		if !((sequence.MinVal == minVal && sequence.Increment < 0) || (sequence.MinVal == 1 && sequence.Increment > 0)) {
			utils.MustPrintln(predataFile, "\tMINVALUE", sequence.MinVal)
		} else {
			utils.MustPrintln(predataFile, "\tNO MINVALUE")
		}
		cycleStr := ""
		if sequence.IsCycled {
			cycleStr = "\n\tCYCLE"
		}
		utils.MustPrintf(predataFile, "\tCACHE %d%s;", sequence.CacheVal, cycleStr)

		utils.MustPrintf(predataFile, "\n\nSELECT pg_catalog.setval('%s', %d, %v);\n", seqFQN, sequence.LastVal, sequence.IsCalled)

		// owningColumn is quoted when the map is constructed in GetSequenceColumnOwnerMap() and doesn't need to be quoted again
		if owningColumn, hasColumnOwner := sequenceColumnOwners[seqFQN]; hasColumnOwner {
			utils.MustPrintf(predataFile, "\n\nALTER SEQUENCE %s OWNED BY %s;\n", seqFQN, owningColumn)
		}
		PrintObjectMetadata(predataFile, sequenceMetadata[sequence.RelationOid], seqFQN, "SEQUENCE")
	}
}

func PrintCreateLanguageStatements(predataFile io.Writer, procLangs []QueryProceduralLanguage,
	funcInfoMap map[uint32]FunctionInfo, procLangMetadata utils.MetadataMap) {
	for _, procLang := range procLangs {
		quotedOwner := utils.QuoteIdent(procLang.Owner)
		quotedLanguage := utils.QuoteIdent(procLang.Name)
		utils.MustPrintf(predataFile, "\n\nCREATE ")
		if procLang.PlTrusted {
			utils.MustPrintf(predataFile, "TRUSTED ")
		}
		utils.MustPrintf(predataFile, "PROCEDURAL LANGUAGE %s;", quotedLanguage)
		/*
		 * If the handler, validator, and inline functions are in pg_pltemplate, we can
		 * dump a CREATE LANGUAGE command without specifying them individually.
		 *
		 * The schema of the handler function should match the schema of the language itself, but
		 * the inline and validator functions can be in a different schema and must be schema-qualified.
		 */

		if procLang.Handler != 0 {
			handlerInfo := funcInfoMap[procLang.Handler]
			utils.MustPrintf(predataFile, "\nALTER FUNCTION %s(%s) OWNER TO %s;", handlerInfo.QualifiedName, handlerInfo.Arguments, quotedOwner)
		}
		if procLang.Inline != 0 {
			inlineInfo := funcInfoMap[procLang.Inline]
			utils.MustPrintf(predataFile, "\nALTER FUNCTION %s(%s) OWNER TO %s;", inlineInfo.QualifiedName, inlineInfo.Arguments, quotedOwner)
		}
		if procLang.Validator != 0 {
			validatorInfo := funcInfoMap[procLang.Validator]
			utils.MustPrintf(predataFile, "\nALTER FUNCTION %s(%s) OWNER TO %s;", validatorInfo.QualifiedName, validatorInfo.Arguments, quotedOwner)
		}
		PrintObjectMetadata(predataFile, procLangMetadata[procLang.LangOid], utils.QuoteIdent(procLang.Name), "LANGUAGE")
		utils.MustPrintln(predataFile)
	}
}

func PrintCreateViewStatements(predataFile io.Writer, views []QueryViewDefinition, viewMetadata utils.MetadataMap) {
	for _, view := range views {
		viewFQN := utils.MakeFQN(view.SchemaName, view.ViewName)
		utils.MustPrintf(predataFile, "\n\nCREATE VIEW %s AS %s\n", viewFQN, view.Definition)
		PrintObjectMetadata(predataFile, viewMetadata[view.ViewOid], viewFQN, "VIEW")
	}
}

func PrintCreateExternalProtocolStatements(predataFile io.Writer, protocols []QueryExtProtocol, funcInfoMap map[uint32]FunctionInfo) {
	for _, protocol := range protocols {

		hasUserDefinedFunc := false
		if function, ok := funcInfoMap[protocol.WriteFunction]; ok && !function.IsInternal {
			hasUserDefinedFunc = true
		}
		if function, ok := funcInfoMap[protocol.ReadFunction]; ok && !function.IsInternal {
			hasUserDefinedFunc = true
		}
		if function, ok := funcInfoMap[protocol.Validator]; ok && !function.IsInternal {
			hasUserDefinedFunc = true
		}

		if !hasUserDefinedFunc {
			continue
		}

		protocolFunctions := []string{}
		if protocol.ReadFunction != 0 {
			protocolFunctions = append(protocolFunctions, fmt.Sprintf("readfunc = %s", funcInfoMap[protocol.ReadFunction].QualifiedName))
		}
		if protocol.WriteFunction != 0 {
			protocolFunctions = append(protocolFunctions, fmt.Sprintf("writefunc = %s", funcInfoMap[protocol.WriteFunction].QualifiedName))
		}
		if protocol.Validator != 0 {
			protocolFunctions = append(protocolFunctions, fmt.Sprintf("validatorfunc = %s", funcInfoMap[protocol.Validator].QualifiedName))
		}

		utils.MustPrintf(predataFile, "\n\nCREATE ")
		if protocol.Trusted {
			utils.MustPrintf(predataFile, "TRUSTED ")
		}
		utils.MustPrintf(predataFile, "PROTOCOL %s (%s);", utils.QuoteIdent(protocol.Name), strings.Join(protocolFunctions, ", "))

		if protocol.Owner != "" {
			utils.MustPrintf(predataFile, "\n\nALTER PROTOCOL %s OWNER TO %s;\n", utils.QuoteIdent(protocol.Name), utils.QuoteIdent(protocol.Owner))
		}
	}
}
