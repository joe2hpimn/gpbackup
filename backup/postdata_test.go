package backup_test

import (
	"github.com/greenplum-db/gpbackup/backup"
	"github.com/greenplum-db/gpbackup/testutils"
	"github.com/greenplum-db/gpbackup/utils"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("backup/postdata tests", func() {
	var buffer *gbytes.Buffer
	BeforeEach(func() {
		buffer = gbytes.NewBuffer()
		testutils.SetupTestLogger()
	})
	Context("Indexes on a single column", func() {
		It("can print a basic index", func() {
			indexes := []backup.QuerySimpleDefinition{{1, "testindex", "public", "testtable", "CREATE INDEX testindex ON public.testtable USING btree(i)"}}
			emptyMetadataMap := utils.MetadataMap{}
			backup.PrintCreateIndexStatements(buffer, indexes, emptyMetadataMap)
			testutils.ExpectRegexp(buffer, `

CREATE INDEX testindex ON public.testtable USING btree(i);`)
		})
		It("can print an index with a comment", func() {
			indexes := []backup.QuerySimpleDefinition{{1, "testindex", "public", "testtable", "CREATE INDEX testindex ON public.testtable USING btree(i)"}}
			indexMetadataMap := utils.MetadataMap{1: {Comment: "This is an index comment."}}
			backup.PrintCreateIndexStatements(buffer, indexes, indexMetadataMap)
			testutils.ExpectRegexp(buffer, `

CREATE INDEX testindex ON public.testtable USING btree(i);

COMMENT ON INDEX testindex IS 'This is an index comment.';`)
		})
	})
})
