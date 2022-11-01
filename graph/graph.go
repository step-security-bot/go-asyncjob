package graph

import (
	"bytes"
)

// NodeConstrain is a constraint for a node in a graph
type NodeConstrain interface {
	DotSpec() *DotNodeSpec
}

// EdgeSpecFunc is a function that returns the DOT specification for an edge.
type EdgeSpecFunc[T NodeConstrain] func(from, to T) *DotEdgeSpec

type Edge[NT NodeConstrain] struct {
	From NT
	To   NT
}

// DotNodeSpec is the specification for a node in a DOT graph
type DotNodeSpec struct {
	ID        string
	Name      string
	Tooltip   string
	Shape     string
	Style     string
	FillColor string
}

// DotEdgeSpec is the specification for an edge in DOT graph
type DotEdgeSpec struct {
	FromNodeID string
	ToNodeID   string
	Tooltip    string
	Style      string
	Color      string
}

// Graph hold the nodes and edges of a graph
type Graph[NT NodeConstrain] struct {
	nodes        map[string]NT
	nodeEdges    map[string][]*Edge[NT]
	edgeSpecFunc EdgeSpecFunc[NT]
}

// NewGraph creates a new graph
func NewGraph[NT NodeConstrain](edgeSpecFunc EdgeSpecFunc[NT]) *Graph[NT] {
	return &Graph[NT]{
		nodes:        make(map[string]NT),
		nodeEdges:    make(map[string][]*Edge[NT]),
		edgeSpecFunc: edgeSpecFunc,
	}
}

// AddNode adds a node to the graph
func (g *Graph[NT]) AddNode(n NT) error {
	nodeKey := n.DotSpec().ID
	if _, ok := g.nodes[nodeKey]; ok {
		return ErrDuplicateNode
	}
	g.nodes[nodeKey] = n

	return nil
}

func (g *Graph[NT]) Connect(from, to string) error {
	var nodeFrom, nodeTo NT
	var ok bool
	if nodeFrom, ok = g.nodes[from]; !ok {
		return ErrConnectNotExistingNode
	}

	if nodeTo, ok = g.nodes[to]; !ok {
		return ErrConnectNotExistingNode
	}

	g.nodeEdges[from] = append(g.nodeEdges[from], &Edge[NT]{From: nodeFrom, To: nodeTo})
	return nil
}

// https://en.wikipedia.org/wiki/DOT_(graph_description_language)
func (g *Graph[NT]) ToDotGraph() (string, error) {
	nodes := make([]*DotNodeSpec, 0)
	for _, node := range g.nodes {
		nodes = append(nodes, node.DotSpec())
	}

	edges := make([]*DotEdgeSpec, 0)
	for _, nodeEdges := range g.nodeEdges {
		for _, edge := range nodeEdges {
			edges = append(edges, g.edgeSpecFunc(edge.From, edge.To))
		}
	}

	buf := new(bytes.Buffer)
	err := digraphTemplate.Execute(buf, templateRef{Nodes: nodes, Edges: edges})
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

type templateRef struct {
	Nodes []*DotNodeSpec
	Edges []*DotEdgeSpec
}
