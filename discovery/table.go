package discovery

import (
	"net"
	"github.com/ethereum/go-ethereum/common"
	"fmt"
	"encoding/binary"
	"crypto/rand"
)

type Table struct {
	count int
	buckets [16]*bucket
	nodeAddedHook func(*Node)
	self *Node
}

type bucket struct {
	entries []*Node
	replacements []*Node
}

func newTable(ourID NodeID, ourAddr *net.UDPAddr) *Table{
	self := NewNode(ourID, ourAddr.IP, uint16(ourAddr.Port), uint16(ourAddr.Port))
	tab := &Table{self:self}
	for i := range tab.buckets {
		tab.buckets[i] = new(bucket)
	}
	return tab
}

func (tab *Table)chooseBucketRefreshTarget() common.Hash{
	entries := 0
	for i, b := range &tab.buckets{
		entries += len(b.entries)
		for _, e := range b.entries {
			fmt.Println(i, e.state, e.addr().String(), e.ID.String(), e.sha.Hex())
		}
	}
	fmt.Println("the all entries num is:", entries)
	prefix := binary.BigEndian.Uint64(tab.self.sha[0:8])
	dist := ^uint64(0)
	entry := int(randUint(uint32(entries + 1)))
	for _, b := range &tab.buckets {
		if entry < len(b.entries) {
			n := b.entries[entry]
			dist = binary.BigEndian.Uint64(n.sha[0:8]) ^ prefix
			break
		}
		entry -= len(b.entries)
	}

	ddist := ^uint64(0)
	if dist+dist > dist {
		ddist = dist
	}
	targetPrefix := prefix ^ randUint64n(ddist)

	var target common.Hash
	binary.BigEndian.PutUint64(target[0:8], targetPrefix)
	rand.Read(target[8:])
	return target
}

func randUint(max uint32) uint32 {
	if max < 2 {
		return 0
	}
	var b [4]byte
	rand.Read(b[:])
	return binary.BigEndian.Uint32(b[:]) % max
}

func randUint64n(max uint64) uint64 {
	if max < 2 {
		return 0
	}
	var b [8]byte
	rand.Read(b[:])
	return binary.BigEndian.Uint64(b[:]) % max
}

func (tab *Table) closest(target common.Hash, nresults int) *nodesByDistance {
	// This is a very wasteful way to find the closest nodes but
	// obviously correct. I believe that tree-based buckets would make
	// this easier to implement efficiently.
	closed := &nodesByDistance{target: target}
	for _, b := range &tab.buckets {
		for _, n := range b.entries {
			closed.push(n, nresults)
		}
	}
	return closed
}

func (tab *Table)stuff(nodes []*Node) {
	outer:
		for _, n := range nodes{
			if n.ID == tab.self.ID{
				continue
			}
			bucket := tab.buckets[logdist(tab.self.sha, n.sha)]
			for i := range bucket.entries {
				if bucket.entries[i].ID == n.ID{
					continue outer
				}
			}
			if len(bucket.entries) < bucketSize{
				bucket.entries = append(bucket.entries, n)
				tab.count++
				if tab.nodeAddedHook != nil {
					tab.nodeAddedHook(n)
				}
			}
		}
}

func (tab *Table)add(n *Node) (contested *Node){
	if n.ID == tab.self.ID {
		return
	}
	b := tab.buckets[logdist(tab.self.sha, n.sha)]
	switch {
	case b.bump(n):
		return nil
	case len(b.entries) < bucketSize:
		b.addFront(n)
		tab.count++
		if tab.nodeAddedHook != nil {
			tab.nodeAddedHook(n)
		}
		return nil
	default:
		b.replacements = append(b.replacements, n)
		if len(b.replacements) > bucketSize {
			copy(b.replacements, b.replacements[1:])
			b.replacements = b.replacements[:len(b.replacements)-1]
		}
		return b.entries[len(b.entries)-1]
	}
}

func (b *bucket) addFront(n *Node) {
	b.entries = append(b.entries, nil)
	copy(b.entries[1:], b.entries)
	b.entries[0] = n
}

func (b *bucket) bump(n *Node) bool {
	for i := range b.entries {
		if b.entries[i].ID == n.ID {
			// move it to the front
			copy(b.entries[1:], b.entries[:i])
			b.entries[0] = n
			return true
		}
	}
	return false
}

func (tab *Table) delete(node *Node) {
	//fmt.Println("delete", node.addr().String(), node.ID.String(), node.sha.Hex())
	bucket := tab.buckets[logdist(tab.self.sha, node.sha)]
	for i := range bucket.entries {
		if bucket.entries[i].ID == node.ID {
			bucket.entries = append(bucket.entries[:i], bucket.entries[i+1:]...)
			tab.count--
			return
		}
	}
}

func (tab *Table) deleteReplace(node *Node) {
	b := tab.buckets[logdist(tab.self.sha, node.sha)]
	i := 0
	for i < len(b.entries) {
		if b.entries[i].ID == node.ID {
			b.entries = append(b.entries[:i], b.entries[i+1:]...)
			tab.count--
		} else {
			i++
		}
	}
	// refill from replacement cache
	// TODO: maybe use random index
	if len(b.entries) < bucketSize && len(b.replacements) > 0 {
		ri := len(b.replacements) - 1
		b.addFront(b.replacements[ri])
		tab.count++
		b.replacements[ri] = nil
		b.replacements = b.replacements[:ri]
	}
}

var lzcount = [256]int{
	8, 7, 6, 6, 5, 5, 5, 5,
	4, 4, 4, 4, 4, 4, 4, 4,
	3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3,
	2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2,
	1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
}

func logdist(a, b common.Hash) int {
	lz := 0
	for i := range a {
		x := a[i] ^ b[i]
		if x == 0 {
			lz += 8
		} else {
			lz += lzcount[x]
			break
		}
	}
	return len(a)*8 - lz
}

