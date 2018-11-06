package discovery

import (
	"time"
	"net"
	"github.com/ethereum/go-ethereum/common"
	"crypto/ecdsa"
	"fmt"
	//"github.com/sirupsen/logrus"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/pkg/errors"
	"sort"
	//"github.com/ethereum/go-ethereum/common/mclock"
	"bytes"
)
const (
	alpha = 3
	bucketSize = 16
	maxFindnodeFailures = 5
)

// timeouts
const (
	respTimeout = 500*time.Millisecond
)

const (
	autoRefreshInterval   = 1 * time.Hour
	bucketRefreshInterval = 1 * time.Minute
	//seedCount             = 30
	//seedMaxAge            = 5 * 24 * time.Hour
	lowPort               = 1024
)

const (

	// Packet type events.
	// These correspond to packet types in the UDP protocol.
	pingPacket = iota + 1
	pongPacket
	findnodePacket
	neighborsPacket
	findnodeHashPacket
	//topicRegisterPacket
	//topicQueryPacket
	//topicNodesPacket

	// Non-packet events.
	// Event values in this category are allocated outside
	// the packet type range (packet types are encoded as a single byte).
	pongTimeout nodeEvent = iota + 256
	pingTimeout
	neighboursTimeout
)

const (
	expiration  = 20 * time.Second
)

type nodeNetGuts struct{
	sha common.Hash
	state *nodeState
	pingEcho          []byte
	pendingNeighbours *findnodeQuery
	deferredQueries []*findnodeQuery
	queryTimeouts int
}

type nodeState struct {
	name     string
	handle   func(*Network, *Node, nodeEvent, *ingressPacket) (next *nodeState, err error)
	enter    func(*Network, *Node)
	canQuery bool
}

func (s *nodeState) String() string {
	return s.name
}

var (
	unknown          *nodeState
	verifyinit       *nodeState
	verifywait       *nodeState
	remoteverifywait *nodeState
	known            *nodeState
	contested        *nodeState
	unresponsive     *nodeState
)


func init() {
	unknown = &nodeState{
		name: "unknown",
		enter: func(net *Network, n *Node) {
			net.tab.delete(n)
			n.pingEcho = nil
			// Abort active queries.
			for _, q := range n.deferredQueries {
				q.reply <- nil
			}
			n.deferredQueries = nil
			if n.pendingNeighbours != nil {
				n.pendingNeighbours.reply <- nil
				n.pendingNeighbours = nil
			}
			n.queryTimeouts = 0
		},
		handle: func(net *Network, n *Node, ev nodeEvent, pkt *ingressPacket) (*nodeState, error) {
			switch ev {
			case pingPacket:
				net.handlePing(n, pkt)
				net.ping(n, pkt.remoteAddr)
				return verifywait, nil
			default:
				return unknown, errors.New("errInvalidEvent")
			}
		},
	}

	verifyinit = &nodeState{
		name: "verifyinit",
		enter: func(net *Network, n *Node) {
			net.ping(n, n.addr())
		},
		handle: func(net *Network, n *Node, ev nodeEvent, pkt *ingressPacket) (*nodeState, error) {
			switch ev {
			case pingPacket:
				net.handlePing(n, pkt)
				return verifywait, nil
			case pongPacket:
				err := net.handleKnownPong(n, pkt)
				return remoteverifywait, err
			case pongTimeout:
				return unknown, nil
			default:
				return verifyinit, errors.New("errInvalidEvent")
			}
		},
	}

	verifywait = &nodeState{
		name: "verifywait",
		handle: func(net *Network, n *Node, ev nodeEvent, pkt *ingressPacket) (*nodeState, error) {
			switch ev {
			case pingPacket:
				net.handlePing(n, pkt)
				return verifywait, nil
			case pongPacket:
				err := net.handleKnownPong(n, pkt)
				return known, err
			case pongTimeout:
				return unknown, nil
			default:
				return verifywait, errors.New("errInvalidEvent")
			}
		},
	}

	remoteverifywait = &nodeState{
		name: "remoteverifywait",
		enter: func(net *Network, n *Node) {
			net.timedEvent(respTimeout, n, pingTimeout)
		},
		handle: func(net *Network, n *Node, ev nodeEvent, pkt *ingressPacket) (*nodeState, error) {
			switch ev {
			case pingPacket:
				net.handlePing(n, pkt)
				return remoteverifywait, nil
			case pingTimeout:
				return known, nil
			default:
				return remoteverifywait, errors.New("errInvalidEvent")
			}
		},
	}

	known = &nodeState{
		name:     "known",
		canQuery: true,
		enter: func(net *Network, n *Node) {
			n.queryTimeouts = 0
			n.startNextQuery(net)
			// Insert into the table and start revalidation of the last node
			// in the bucket if it is full.
			last := net.tab.add(n)
			if last != nil && last.state == known {
				// TODO: do this asynchronously
				net.transition(last, contested)
			}
		},
		handle: func(net *Network, n *Node, ev nodeEvent, pkt *ingressPacket) (*nodeState, error) {
			switch ev {
			case pingPacket:
				net.handlePing(n, pkt)
				return known, nil
			case pongPacket:
				err := net.handleKnownPong(n, pkt)
				return known, err
			default:
				return net.handleQueryEvent(n, ev, pkt)
			}
		},
	}

	contested = &nodeState{
		name:     "contested",
		canQuery: true,
		enter: func(net *Network, n *Node) {
			net.ping(n, n.addr())
		},
		handle: func(net *Network, n *Node, ev nodeEvent, pkt *ingressPacket) (*nodeState, error) {
			switch ev {
			case pongPacket:
				// Node is still alive.
				err := net.handleKnownPong(n, pkt)
				return known, err
			case pongTimeout:
				net.tab.deleteReplace(n)
				return unresponsive, nil
			case pingPacket:
				net.handlePing(n, pkt)
				return contested, nil
			default:
				return net.handleQueryEvent(n, ev, pkt)
			}
		},
	}

	unresponsive = &nodeState{
		name:     "unresponsive",
		canQuery: true,
		handle: func(net *Network, n *Node, ev nodeEvent, pkt *ingressPacket) (*nodeState, error) {
			switch ev {
			case pingPacket:
				net.handlePing(n, pkt)
				return known, nil
			case pongPacket:
				err := net.handleKnownPong(n, pkt)
				return known, err
			default:
				return net.handleQueryEvent(n, ev, pkt)
			}
		},
	}
}

type nodeEvent uint

type ingressPacket struct {
	remoteID   NodeID
	remoteAddr *net.UDPAddr
	ev         nodeEvent
	hash       []byte
	data       interface{} // one of the RPC structs
	rawData    []byte
}

type timeoutEvent struct {
	ev   nodeEvent
	node *Node
}

type findnodeQuery struct {
	remote   *Node
	target   common.Hash
	reply    chan<- []*Node
	nresults int // counter for received nodes
}

type Network struct {
	//db          *nodeDB // database of known nodes
	conn        transport
	//netrestrict *netutil.Netlist

	closed           chan struct{}          // closed when loop is done
	closeReq         chan struct{}          // 'request to close'
	refreshReq       chan []*Node           // lookups ask for refresh on this channel
	refreshResp      chan (<-chan struct{}) // ...and get the channel to block on from this one
	read             chan ingressPacket     // ingress packets arrive here
	timeout          chan timeoutEvent
	queryReq         chan *findnodeQuery // lookups submit findnode queries on this channel
	tableOpReq       chan func()
	tableOpResp      chan struct{}
	//topicRegisterReq chan topicRegisterReq
	//topicSearchReq   chan topicSearchReq

	// State of the main loop.
	tab           *Table
	//topictab      *topicTable
	//ticketStore   *ticketStore
	nursery       []*Node
	nodes         map[NodeID]*Node // tracks active nodes with state != known
	timeoutTimers map[timeoutEvent]*time.Timer

	// Revalidation queues.
	// Nodes put on these queues will be pinged eventually.
	slowRevalidateQueue []*Node
	fastRevalidateQueue []*Node

	// Buffers for state transition.
	sendBuf []*ingressPacket
}

type transport interface {
	sendPing(remote *Node, remoteAddr *net.UDPAddr) (hash []byte)
	sendNeighbours(remote *Node, nodes []*Node)
	sendFindnodeHash(remote *Node, target common.Hash)
	//sendTopicRegister(remote *Node, topics []Topic, topicIdx int, pong []byte)
	//sendTopicNodes(remote *Node, queryHash common.Hash, nodes []*Node)

	send(remote *Node, ptype nodeEvent, p interface{}) (hash []byte)

	localAddr() *net.UDPAddr
	Close()
}

func newNetwork(conn transport, ourPubkey ecdsa.PublicKey, dbPath string) (*Network, error){
	ourID := PubkeyID(&ourPubkey)
	tab := newTable(ourID, conn.localAddr())
	net := &Network{
		//db:               db,
		conn:             conn,
		//netrestrict:      netrestrict,
		tab:              tab,
		//topictab:         newTopicTable(db, tab.self),
		//ticketStore:      newTicketStore(),
		refreshReq:       make(chan []*Node),
		refreshResp:      make(chan (<-chan struct{})),
		closed:           make(chan struct{}),
		closeReq:         make(chan struct{}),
		read:             make(chan ingressPacket, 100),
		timeout:          make(chan timeoutEvent),
		timeoutTimers:    make(map[timeoutEvent]*time.Timer),
		tableOpReq:       make(chan func()),
		tableOpResp:      make(chan struct{}),
		queryReq:         make(chan *findnodeQuery),
		//topicRegisterReq: make(chan topicRegisterReq),
		//topicSearchReq:   make(chan topicSearchReq),
		nodes:            make(map[NodeID]*Node),
	}
	go net.loop()
	return net, nil
}

func (net *Network)loop() {
	var (
		refreshTimer = time.NewTicker(autoRefreshInterval)
		bucketRefreshTimer = time.NewTimer(bucketRefreshInterval)
		refreshDone chan struct{}
	)

loop:
	for {
		//resetNextTicket()
		select {
		case <-net.closeReq:
			log.Trace("<-net.closeReq")
			break loop
			// Ingress packet handling.
		case pkt := <-net.read:
			fmt.Println("read", pkt.ev)
			log.Trace("<-net.read")
			n := net.internNode(&pkt)
			prestate := n.state
			status := "ok"
			if err := net.handle(n, pkt.ev, &pkt); err != nil {
				status = err.Error()
				fmt.Println(status)
			}
			log.Trace("", "msg", log.Lazy{Fn: func() string {
				return fmt.Sprintf("<<< (%d) %v from %x@%v: %v -> %v (%v)",
					net.tab.count, pkt.ev, pkt.remoteID[:8], pkt.remoteAddr, prestate, n.state, status)
			}})
			// TODO: persist state if n.state goes >= known, delete if it goes <= known

			// State transition timeouts.
		case timeout := <-net.timeout:
			log.Trace("<-net.timeout")
			if net.timeoutTimers[timeout] == nil {
				// Stale timer (was aborted).
				continue
			}
			delete(net.timeoutTimers, timeout)
			prestate := timeout.node.state
			status := "ok"
			if err := net.handle(timeout.node, timeout.ev, nil); err != nil {
				status = err.Error()
			}
			log.Trace("", "msg", log.Lazy{Fn: func() string {
				return fmt.Sprintf("--- (%d) %v for %x@%v: %v -> %v (%v)",
					net.tab.count, timeout.ev, timeout.node.ID[:8], timeout.node.addr(), prestate, timeout.node.state, status)
			}})

			// Querying.
		case q := <-net.queryReq:
			log.Trace("<-net.queryReq")
			if !q.start(net) {
				q.remote.deferQuery(q)
			}

			// Interacting with the table.
		case f := <-net.tableOpReq:
			log.Trace("<-net.tableOpReq")
			f()
			net.tableOpResp <- struct{}{}

			// Periodic / lookup-initiated bucket refresh.
		case <-refreshTimer.C:
			log.Trace("<-refreshTimer.C")
			// TODO: ideally we would start the refresh timer after
			// fallback nodes have been set for the first time.
			if refreshDone == nil {
				refreshDone = make(chan struct{})
				net.refresh(refreshDone)
			}
		case <-bucketRefreshTimer.C:
			target := net.tab.chooseBucketRefreshTarget()
			go func() {
				net.lookup(target, false)
				bucketRefreshTimer.Reset(bucketRefreshInterval)
			}()
		case newNursery := <-net.refreshReq:
			log.Trace("<-net.refreshReq")
			if newNursery != nil {
				net.nursery = newNursery
			}
			if refreshDone == nil {
				refreshDone = make(chan struct{})
				net.refresh(refreshDone)
			}
			net.refreshResp <- refreshDone
		case <-refreshDone:
			log.Trace("<-net.refreshDone", "table size", net.tab.count)
			if net.tab.count != 0 {
				refreshDone = nil
			} else {
				refreshDone = make(chan struct{})
				net.refresh(refreshDone)
			}
		}
	}
	log.Trace("loop stopped")

	//log.Debug(fmt.Sprintf("shutting down"))
	if net.conn != nil {
		net.conn.Close()
	}
	if refreshDone != nil {
		// TODO: wait for pending refresh.
		//<-refreshResults
	}
	// Cancel all pending timeouts.
	for _, timer := range net.timeoutTimers {
		timer.Stop()
	}
	//if net.db != nil {
	//	net.db.close()
	//}
	close(net.closed)
}

func (net *Network) internNode(pkt *ingressPacket) *Node {
	if n := net.nodes[pkt.remoteID]; n != nil {
		n.IP = pkt.remoteAddr.IP
		n.UDP = uint16(pkt.remoteAddr.Port)
		n.TCP = uint16(pkt.remoteAddr.Port)
		return n
	}
	n := NewNode(pkt.remoteID, pkt.remoteAddr.IP, uint16(pkt.remoteAddr.Port), uint16(pkt.remoteAddr.Port))
	n.state = unknown
	net.nodes[pkt.remoteID] = n
	return n
}

func (net *Network)internNodeFromDB(dbn *Node) *Node {
	if n := net.nodes[dbn.ID]; n != nil {
		return n
	}
	n := NewNode(dbn.ID, dbn.IP, dbn.UDP, dbn.TCP)
	n.state = unknown
	net.nodes[dbn.ID] = n
	return n
}

func (net *Network)handle(n *Node, ev nodeEvent, pkt *ingressPacket) error {
	if pkt != nil {
		if err := net.checkPacket(n, ev, pkt); err != nil {
			return err
		}
		//if net.db != nil {
		//	net.db.ensureExpirer()
		//}
	}
	if n.state == nil {
		n.state = unknown
	}
	next, err := n.state.handle(net,n,ev,pkt)
	net.transition(n, next)
	fmt.Println(next)
	return err

}

func (net *Network) abortTimedEvent(n *Node, ev nodeEvent) {
	timer := net.timeoutTimers[timeoutEvent{ev, n}]
	if timer != nil {
		timer.Stop()
		delete(net.timeoutTimers, timeoutEvent{ev, n})
	}
}

func (net *Network)ping(n *Node, addr *net.UDPAddr) {
	if n.pingEcho != nil || n.ID == net.tab.self.ID {
		return
	}
	n.pingEcho = net.conn.sendPing(n, addr)
	net.timedEvent(respTimeout, n, pongTimeout)
}

func (net *Network)handlePing(n *Node, pkt *ingressPacket) {
	ping := pkt.data.(*ping)
	n.TCP = ping.From.TCP
	pong := &pong{
		makeEndpoint(n.addr(), n.TCP),
		pkt.hash,
		uint64(time.Now().Add(expiration).Unix())}
	net.conn.send( n, pongPacket, pong)
}

// TODO by zhiyong
func (net *Network)handleKnownPong(n *Node, pkt *ingressPacket) error {
	log.Trace("Handling known pong", "node", n.ID)
	net.abortTimedEvent(n, pongTimeout)
	//now := time.Now().Unix()
	//ticket, err := pongToTicket(now, n.pingTopics, n, pkt)
	//if err == nil {
	//	// fmt.Printf("(%x) ticket: %+v\n", net.tab.self.ID[:8], pkt.data)
	//	//net.ticketStore.addTicket(now, pkt.data.(*pong).ReplyTok, ticket)
	//} else {
	//	log.Trace("Failed to convert pong to ticket", "err", err)
	//}
	n.pingEcho = nil
	return nil
}

func (net *Network) handleQueryEvent(n *Node, ev nodeEvent, pkt *ingressPacket) (*nodeState, error) {
	switch ev {
	case findnodePacket:
		target := crypto.Keccak256Hash(pkt.data.(*findnode).Target[:])
		results := net.tab.closest(target, bucketSize).entries
		net.conn.sendNeighbours(n, results)
		return n.state, nil
	case neighborsPacket:
		err := net.handleNeighboursPacket(n, pkt)
		return n.state, err
	case neighboursTimeout:
		if n.pendingNeighbours != nil {
			n.pendingNeighbours.reply <- nil
			n.pendingNeighbours = nil
		}
		n.queryTimeouts++
		if n.queryTimeouts > maxFindnodeFailures && n.state == known {
			return contested, errors.New("too many timeouts")
		}
		return n.state, nil

		// v5

	case findnodeHashPacket:
		results := net.tab.closest(pkt.data.(*findnodeHash).Target, bucketSize).entries
		net.conn.sendNeighbours(n, results)
		return n.state, nil
	default:
		return n.state, errors.New("errInvalidEvent")
	}
}

func (net *Network)reqReadPacket(pkt ingressPacket) {
	select {
	case net.read <- pkt:
	case <-net.closed:
	}
}

func (net *Network)reqRefresh(nursery []*Node) <-chan struct{}{
	select {
	case net.refreshReq <- nursery:
		return <-net.refreshResp
	case <-net.closed:
		return net.closed
	}
}

func (net *Network)SetFallbackNodes(nodes []*Node) error {
	nursery := make([]*Node, 0, len(nodes))
	for _, n := range nodes {
		if n == nil {
			continue
		}
		if err := n.validateComplete(); err != nil {
			return fmt.Errorf("bad bootstrap/fallback node %v (%v)", n, err)
		}
		cpy := *n
		cpy.sha = crypto.Keccak256Hash(n.ID[:])
		nursery = append(nursery, &cpy)
	}
	net.reqRefresh(nursery)
	return nil
}

func (net *Network)refresh(done chan<- struct{}) {
	var seeds []*Node
	if len(seeds) == 0 {
		seeds = net.nursery
	}

	if len(seeds) == 0 {
		time.AfterFunc(time.Second*10, func() {
			close(done)
		})
		return
	}

	for _, n := range seeds {
		n = net.internNodeFromDB(n)
		if n.state == unknown {
			net.transition(n, verifyinit)
		}
		net.tab.add(n)
	}
	go func(){
		net.Lookup(net.tab.self.ID)
		close(done)
	}()
}

func (net *Network)Lookup(targetID NodeID) []*Node{
	return net.lookup(crypto.Keccak256Hash(targetID[:]), false)
}

func (net *Network) lookup(target common.Hash, stopOnMatch bool) []*Node {
	var (
		asked          = make(map[NodeID]bool)
		seen           = make(map[NodeID]bool)
		reply          = make(chan []*Node, alpha)
		result         = nodesByDistance{target: target}
		pendingQueries = 0
	)
	// Get initial answers from the local node.
	result.push(net.tab.self, bucketSize)
	for {
		// Ask the α closest nodes that we haven't asked yet.
		for i := 0; i < len(result.entries) && pendingQueries < alpha; i++ {
			n := result.entries[i]
			if !asked[n.ID] {
				asked[n.ID] = true
				pendingQueries++
				net.reqQueryFindnode(n, target, reply)
			}
		}
		if pendingQueries == 0 {
			// We have asked all closest nodes, stop the search.
			break
		}
		// Wait for the next reply.
		select {
		case nodes := <-reply:
			for _, n := range nodes {
				if n != nil && !seen[n.ID] {
					seen[n.ID] = true
					result.push(n, bucketSize)
					if stopOnMatch && n.sha == target {
						return result.entries
					}
				}
			}
			pendingQueries--
		case <-time.After(respTimeout):
			// forget all pending requests, start new ones
			pendingQueries = 0
			reply = make(chan []*Node, alpha)
		}
	}
	return result.entries
}

func (net *Network)reqQueryFindnode(n *Node, target common.Hash, reply chan []*Node) bool {
	q := &findnodeQuery{remote:n, target: target, reply: reply}
	select {
	case net.queryReq <- q:
		return true
	case <- net.closed:
		return false
	}
}

func (n *nodeNetGuts) deferQuery(q * findnodeQuery){
	n.deferredQueries = append(n.deferredQueries, q)
}

func (n *nodeNetGuts) startNextQuery(net *Network) {
	if len(n.deferredQueries) == 0 {
		return
	}
	nextq := n.deferredQueries[0]
	if nextq.start(net) {
		n.deferredQueries = append(n.deferredQueries[:0], n.deferredQueries[1:]...)
	}
}

func (q *findnodeQuery) start(net *Network)bool {
	if q.remote == net.tab.self {
		closest := net.tab.closest(crypto.Keccak256Hash(q.target[:]), bucketSize)
		q.reply <- closest.entries
		return true
	}
	if q.remote.state.canQuery && q.remote.pendingNeighbours == nil {
		net.conn.sendFindnodeHash(q.remote, q.target)
		net.timedEvent(respTimeout, q.remote, neighboursTimeout)
		q.remote.pendingNeighbours = q
		return true
	}
	if q.remote.state == unknown {
		net.transition(q.remote, verifyinit)
	}
	return false
}

func (net *Network) timedEvent(d time.Duration, n *Node, ev nodeEvent) {
	timeout := timeoutEvent{ev, n}
	net.timeoutTimers[timeout] = time.AfterFunc(d, func() {
		select {
		case net.timeout <- timeout:
		case <-net.closed:
		}
	})
}

func (net *Network) transition(n *Node, next *nodeState) {
	if n.state != next {
		n.state = next
		if next.enter != nil {
			next.enter(net, n)
		}
	}
	// TODO: persist/unpersist node
}

type nodesByDistance struct {
	entries []*Node
	target  common.Hash
}

// push adds the given node to the list, keeping the total size below maxElems.
func (h *nodesByDistance) push(n *Node, maxElems int) {
	ix := sort.Search(len(h.entries), func(i int) bool {
		return distcmp(h.target, h.entries[i].sha, n.sha) > 0
	})
	if len(h.entries) < maxElems {
		h.entries = append(h.entries, n)
	}
	if ix == len(h.entries) {
		// farther away than all nodes we already have.
		// if there was room for it, the node is now the last element.
	} else {
		// slide existing entries down to make room
		// this will overwrite the entry we just appended.
		copy(h.entries[ix+1:], h.entries[ix:])
		h.entries[ix] = n
	}
}

func (net *Network)handleNeighboursPacket(n *Node, pkt *ingressPacket) error {
	if n.pendingNeighbours == nil {
		return errors.New("errNoquery")
	}
	net.abortTimedEvent(n, neighboursTimeout)
	req := pkt.data.(*neighbors)
	nodes := make([]*Node, len(req.Nodes))
	for i, rn := range req.Nodes {
		nn, err := net.internNodeFromNeighbours(pkt.remoteAddr, rn)
		if err != nil {
			log.Debug(fmt.Sprintf("invalid neighbour (%v) from %x@%v: %v", rn.IP, n.ID[:8], pkt.remoteAddr, err))
			continue
		}
		nodes[i] = nn
		if nn.state == unknown {
			net.transition(nn, verifyinit)
		}
	}
	n.pendingNeighbours.reply <- nodes
	n.pendingNeighbours = nil
	n.startNextQuery(net)
	return nil
}

func (net *Network) internNodeFromNeighbours(sender *net.UDPAddr, rn rpcNode) (n *Node, err error) {
	if rn.ID == net.tab.self.ID {
		return nil, errors.New("is self")
	}
	if rn.UDP <= lowPort {
		return nil, errors.New("low port")
	}
	n = net.nodes[rn.ID]
	if n == nil {
		// We haven't seen this node before.
		n, err = nodeFromRPC(sender, rn)
		//if net.netrestrict != nil && !net.netrestrict.Contains(n.IP) {
		//	return n, errors.New("not contained in netrestrict whitelist")
		//}
		if err == nil {
			n.state = unknown
			net.nodes[n.ID] = n
		}
		return n, err
	}
	if !n.IP.Equal(rn.IP) || n.UDP != rn.UDP || n.TCP != rn.TCP {
		if n.state == known {
			// reject address change if node is known by us
			err = fmt.Errorf("metadata mismatch: got %v, want %v", rn, n)
		} else {
			// accept otherwise; this will be handled nicer with signed ENRs
			n.IP = rn.IP
			n.UDP = rn.UDP
			n.TCP = rn.TCP
		}
	}
	return n, err
}


func (net *Network) checkPacket(n *Node, ev nodeEvent, pkt *ingressPacket) error {
	// Replay prevention checks.
	switch ev {
	case pingPacket, findnodeHashPacket, neighborsPacket:
		// TODO: check date is > last date seen
		// TODO: check ping version
	case pongPacket:
		if !bytes.Equal(pkt.data.(*pong).ReplyTok, n.pingEcho) {
			// fmt.Println("pong reply token mismatch")
			return fmt.Errorf("pong reply token mismatch")
		}
		n.pingEcho = nil
	}
	// Address validation.
	// TODO: Ideally we would do the following:
	//  - reject all packets with wrong address except ping.
	//  - for ping with new address, transition to verifywait but keep the
	//    previous node (with old address) around. if the new one reaches known,
	//    swap it out.
	return nil
}