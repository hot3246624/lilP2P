package discovery

import (
	"github.com/ethereum/go-ethereum/common"
	"net"
	"crypto/ecdsa"
	"fmt"
	"bytes"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/crypto"
	"time"
	"studyP2P/netutil"
	"github.com/pkg/errors"
	"github.com/ethereum/go-ethereum/log"
)

const Version = 6

type (
	ping struct {
		Version uint
		From, To rpcEndpoint
		Expiration uint64
	}

	pong struct {
		To rpcEndpoint
		ReplyTok []byte
		Expiration uint64
	}

	findnode struct{
		Target NodeID
		Expiration uint64
	}

	findnodeHash struct {
		Target common.Hash
		Expiration uint64
	}

	neighbors struct {
		Nodes []rpcNode
		Expiration uint64
	}

	rpcNode struct {
		IP net.IP
		UDP uint16
		TCP uint16
		ID NodeID
	}

	rpcEndpoint struct {
		IP net.IP
		UDP uint16
		TCP uint16
	}
)

type conn interface {
	ReadFromUDP(b []byte) (n int, addr *net.UDPAddr, err error)
	WriteToUDP(b []byte, addr *net.UDPAddr) (n int, err error)
	Close() error
	LocalAddr() net.Addr
}

type udp struct {
	conn conn
	priv *ecdsa.PrivateKey
	ourEndpoint rpcEndpoint
	net *Network
}

func ListenUDP(priv *ecdsa.PrivateKey, conn conn, nodeDBPath string) (*Network, error){
	realaddr := conn.LocalAddr().(*net.UDPAddr)
	transport, err := listenUDP(priv, conn, realaddr)
	if err != nil {
		return nil, err
	}
	net, err := newNetwork(transport, priv.PublicKey, "")
	if err != nil {
		return nil, err
	}
	transport.net = net
	go transport.readLoop()
	return net, nil
}

func listenUDP(priv *ecdsa.PrivateKey, conn conn, realaddr *net.UDPAddr) (*udp, error) {
	return &udp{conn: conn, priv: priv, ourEndpoint: makeEndpoint(realaddr, uint16(realaddr.Port))}, nil
}

func makeEndpoint(addr *net.UDPAddr, tcpPort uint16) rpcEndpoint {
	ip := addr.IP.To4()
	if ip == nil {
		ip = addr.IP.To16()
	}
	return rpcEndpoint{IP: ip, UDP: uint16(addr.Port), TCP: tcpPort}
}

func (t *udp)send(remote *Node, ptype nodeEvent, data interface{}) (hash []byte){
	hash, _ = t.sendPacket(remote.ID, remote.addr(), byte(ptype), data)
	return hash
}

func (t *udp)sendPing(remote *Node, toaddr *net.UDPAddr) (hash []byte){
	hash, _ = t.sendPacket(remote.ID, remote.addr(), byte(pingPacket), ping{
		Version: Version,
		From:t.ourEndpoint,
		To:makeEndpoint(toaddr, uint16(toaddr.Port)),
		Expiration:uint64(time.Now().Add(expiration).Unix()),
	})
	return hash
}

func (t *udp)sendNeighbours(remote *Node, results []*Node) {
	p := neighbors{Expiration: uint64(time.Now().Add(expiration).Unix())}
	for i, result := range results {
		p.Nodes = append(p.Nodes, nodeToRPC(result))
		if len(p.Nodes) == maxNeighbors || i == len(results) - 1 {
			t.sendPacket(remote.ID, remote.addr(), byte(neighborsPacket), p)
			p.Nodes = p.Nodes[:0]
		}
	}

}

func (t *udp)sendFindnodeHash(remote *Node, target common.Hash){
	t.sendPacket(remote.ID, remote.addr(), byte(findnodeHashPacket), findnodeHash{
		Target:     target,
		Expiration: uint64(time.Now().Add(expiration).Unix()),
	})
}

func (t *udp)Close() {
	t.conn.Close()
}

func (t *udp)localAddr() *net.UDPAddr{
	return t.conn.LocalAddr().(*net.UDPAddr)
}

func (t *udp)sendPacket(toid NodeID, toaddr *net.UDPAddr, ptype byte, req interface{}) (hash []byte, err error) {
	packet, hash, err := encodePacket(t.priv, ptype, req)
	if err != nil {
		return hash, nil
	}

	if nbytes, err := t.conn.WriteToUDP(packet, toaddr); err != nil {
		fmt.Println("UDP send failed: ", err)
	}else{
		fmt.Println("UDP send successed: ", nbytes)
	}
	return hash, err
}

func (t *udp)readLoop(){
	defer t.conn.Close()
	buf := make([]byte, 1280)
	for {
		nbytes, from, err := t.conn.ReadFromUDP(buf)
		if netutil.IsTemporaryError(err){
			log.Info(fmt.Sprintf("Temporary read error: %v", err))
			continue
		}else if err != nil {
			log.Debug(fmt.Sprintf("Read error: %v", err))
			return
		}
		t.handlePacket(from, buf[:nbytes])
	}
}

func (t *udp)handlePacket(from *net.UDPAddr, buf []byte) error {
	pkt := ingressPacket{remoteAddr:from}
	if err := decodePacket(buf, &pkt); err != nil {
		log.Debug(fmt.Sprintf("Bad packet from %v: %v", from, err))
		return err
	}
	t.net.reqReadPacket(pkt)
	return nil
}

//util methods
var (
	versionPrefix     = []byte("nokodemion discovery")
	versionPrefixSize = len(versionPrefix)
	sigSize           = 520 / 8
	headSize          = versionPrefixSize + sigSize // space of packet frame data
)

var headSpace = make([]byte, headSize)

//最大的邻居节点个数
var maxNeighbors = func() int {
	p := neighbors{Expiration: ^uint64(0)}
	maxSizeNode := rpcNode{IP: make(net.IP, 16), UDP: ^uint16(0), TCP: ^uint16(0)}
	for n := 0; ; n++ {
		p.Nodes = append(p.Nodes, maxSizeNode)
		size, _, err := rlp.EncodeToReader(p)
		if err != nil {
			// If this ever happens, it will be caught by the unit tests.
			panic("cannot encode: " + err.Error())
		}
		if headSize+size+1 >= 1280 {
			return n
		}
	}
}()

func encodePacket(priv *ecdsa.PrivateKey, ptype byte, req interface{}) (p, hash []byte, err error) {
	b := new(bytes.Buffer)
	b.Write(headSpace)
	b.WriteByte(ptype)
	if err := rlp.Encode(b, req); err != nil {
		//log.Error(fmt.Sprint("error encoding packet:", err))
		return nil, nil, err
	}
	packet := b.Bytes()
	sig, err := crypto.Sign(crypto.Keccak256(packet[headSize:]), priv)
	if err != nil {
		//log.Error(fmt.Sprint("could not sign packet:", err))
		return nil, nil, err
	}
	copy(packet, versionPrefix)
	copy(packet[versionPrefixSize:], sig)
	hash = crypto.Keccak256(packet[versionPrefixSize:])
	return packet, hash, nil
}

func decodePacket(buffer []byte, pkt *ingressPacket) error {
	if len(buffer) < headSize + 1 {
		return errors.New("packer too small")
	}

	buf := make([]byte, len(buffer))
	copy(buf, buffer)
	prefix, sig, sigdata := buf[:versionPrefixSize], buf[versionPrefixSize:headSize], buf[headSize:]
	if !bytes.Equal(prefix, versionPrefix) {
		return errors.New("bad prefix")
	}

	fromID, err := recoverNodeID(crypto.Keccak256(sigdata), sig)
	if err != nil {
		return err
	}

	pkt.rawData = buf
	pkt.hash = crypto.Keccak256(buf[versionPrefixSize:])
	pkt.remoteID = fromID
	switch pkt.ev = nodeEvent(sigdata[0]); pkt.ev {
	case pingPacket:
		pkt.data = new(ping)
	case pongPacket:
		pkt.data = new(pong)
	case findnodePacket:
		pkt.data = new(findnode)
	case neighborsPacket:
		pkt.data = new(neighbors)
	case findnodeHashPacket:
		pkt.data = new(findnodeHash)
	default:
		return fmt.Errorf("unknown packet type: %d", sigdata[0])
	}
	s := rlp.NewStream(bytes.NewReader(sigdata[1:]),0)
	err = s.Decode(pkt.data)
	return err
}

func recoverNodeID(hash, sig []byte) (id NodeID, err error) {
	pubkey, err := crypto.Ecrecover(hash, sig)
	if err != nil {
		return id, err
	}

	if len(pubkey)-1 != len(id){
		return id, fmt.Errorf("recovered pubkey has %d bits, want %d bits", len(pubkey)*8, (len(id)+1)*8)
	}

	for i := range id {
		id[i] = pubkey[i+1]
	}
	return id,nil
}

func nodeToRPC(n *Node) rpcNode{
	return rpcNode{ID: n.ID, IP: n.IP, UDP: n.UDP, TCP: n.TCP}
}

func nodeFromRPC(sender *net.UDPAddr, rn rpcNode) (*Node, error) {
	if err := netutil.CheckRelayIP(sender.IP, rn.IP); err != nil {
		return nil, err
	}
	n := NewNode(rn.ID, rn.IP, rn.UDP, rn.TCP)
	err := n.validateComplete()
	return n, err
}

type ReadPacket struct {
	Data []byte
	Addr *net.UDPAddr
}