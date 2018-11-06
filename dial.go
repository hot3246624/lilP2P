package studyP2P

import "studyP2P/discovery"

type discoveryTable interface {
	Close()
	Resolve(*discovery.Node) *discovery.Node
	LookupRandom() []*discovery.Node
	ReadRandomNodes([]*discovery.Node) int
}