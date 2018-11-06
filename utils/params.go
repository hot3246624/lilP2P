package utils

import (
	"studyP2P/discovery"
	"studyP2P/params"
)

func FoundationBootnodes() []*discovery.Node {
	nodes := make([]*discovery.Node, len(params.DiscoveryBootnodesForTest))
	for _, url := range params.DiscoveryBootnodesForTest {
		nodes = append(nodes, discovery.MustParseNode(url))
	}
	return nodes
}