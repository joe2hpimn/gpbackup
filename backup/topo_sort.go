package backup

/*
 * This file contains structs and functions related to dumping data on the segments.
 */

var ()

type Graph struct {
	Nodes         []uint32
	Edges         map[uint32][]uint32
	IncomingEdges map[uint32]int
}

func (g *Graph) RemoveEdge(fromOid uint32, idx int) {
	toNodes := g.Edges[fromOid]
	nodeToRemove := toNodes[idx]
	toNodes[idx], toNodes[len(toNodes)-1] = toNodes[len(toNodes)-1], toNodes[idx]
	toNodes = toNodes[:len(toNodes)-1]
	if len(toNodes) == 0 {
		delete(g.Edges, fromOid)
	} else {
		g.Edges[fromOid] = toNodes
	}
	g.IncomingEdges[nodeToRemove]--
}

func (g *Graph) TopoSort() ([]uint32, bool) {
	var (
		sortedNodes []uint32
		startNodes  []uint32
	)

	for i := 0; i < len(g.Nodes); i++ {
		if g.IncomingEdges[g.Nodes[i]] == 0 {
			startNodes = append(startNodes, g.Nodes[i])
		}
	}

	for len(startNodes) > 0 {
		fromOid := startNodes[0]
		sortedNodes = append(sortedNodes, fromOid)
		startNodes = startNodes[1:]
		for i, toOid := range g.Edges[fromOid] {
			g.RemoveEdge(fromOid, i)
			if g.IncomingEdges[toOid] == 0 {
				startNodes = append(startNodes, toOid)
			}
		}
	}

	if len(g.Edges) > 0 {
		return sortedNodes, false
	} else {
		return sortedNodes, true
	}
}
