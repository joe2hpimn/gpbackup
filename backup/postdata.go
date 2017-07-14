package backup

/*
 * This file contains structs and functions related to dumping "post-data" metadata
 * on the master, which is any metadata that needs to be restored after data is
 * restored, such as indexes and rules.
 */

import (
	"fmt"
	"io"
	"sort"

	"github.com/greenplum-db/gpbackup/utils"
)

func PrintCreateIndexStatements(postdataFile io.Writer, indexes []QuerySimpleDefinition, indexMetadata utils.MetadataMap) {
	for _, index := range indexes {
		utils.MustPrintf(postdataFile, "\n\n%s;", index.Def)
		PrintObjectMetadata(postdataFile, indexMetadata[index.Oid], index.Name, "INDEX")
	}
}

func GetRuleDefinitions(connection *utils.DBConn) []string {
	rules := make([]string, 0)
	ruleList := GetRuleMetadata(connection)
	for _, rule := range ruleList {
		ruleStr := fmt.Sprintf("\n\n%s", rule.Def)
		/*if rule.Comment != "" {
			tableFQN := utils.MakeFQN(rule.OwningSchema, rule.OwningTable)
			ruleStr += fmt.Sprintf("\nCOMMENT ON RULE %s ON %s IS '%s';", utils.QuoteIdent(rule.Name), tableFQN, rule.Comment)
		}*/
		rules = append(rules, ruleStr)
	}
	return rules
}

func GetTriggerDefinitions(connection *utils.DBConn) []string {
	triggers := make([]string, 0)
	triggerList := GetTriggerMetadata(connection)
	for _, trigger := range triggerList {
		triggerStr := fmt.Sprintf("\n\n%s;", trigger.Def)
		/*if trigger.Comment != "" {
			tableFQN := utils.MakeFQN(trigger.OwningSchema, trigger.OwningTable)
			triggerStr += fmt.Sprintf("\nCOMMENT ON TRIGGER %s ON %s IS '%s';", utils.QuoteIdent(trigger.Name), tableFQN, trigger.Comment)
		}*/
		triggers = append(triggers, triggerStr)
	}
	return triggers
}

func PrintPostdataCreateStatements(postdataFile io.Writer, statements []string) {
	sort.Strings(statements)
	for _, statement := range statements {
		utils.MustPrintln(postdataFile, statement)
	}
}
