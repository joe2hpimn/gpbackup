package backup_test

import (
	"gpbackup/backup"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("backup/data tests", func() {
	Describe("topoSort", func() {
		It("Basic sort", func() {
			nodes := []uint32{1, 2}
			edges := map[uint32][]uint32{1: {2}}
			incoming := map[uint32]int{2: 1, 1: 0}
			g := backup.Graph{nodes, edges, incoming}
			sortedList, ok := g.TopoSort()
			Expect(ok).To(BeTrue())
			Expect(sortedList).To(Equal([]uint32{1, 2}))
		})
		It("Sort with cycle", func() {
			nodes := []uint32{1, 2}
			edges := map[uint32][]uint32{1: {2}, 2: {1}}
			incoming := map[uint32]int{2: 1, 1: 1}
			g := backup.Graph{nodes, edges, incoming}
			sortedList, ok := g.TopoSort()
			Expect(ok).To(BeFalse())
			Expect(sortedList).To(BeNil())
		})
	})
	Describe("remove edge", func() {
		It("removes an edge", func() {
			nodes := []uint32{1, 2, 3}
			edges := map[uint32][]uint32{1: {2}, 2: {3}}
			incoming := map[uint32]int{2: 1, 1: 0, 3: 1}
			g := backup.Graph{nodes, edges, incoming}
			g.RemoveEdge(1, 0)
		})
	})
})
