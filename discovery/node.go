package discovery

import (
	"net"
	"crypto/ecdsa"
	"crypto/elliptic"
	"fmt"
	"github.com/pkg/errors"
	"github.com/ethereum/go-ethereum/crypto"
	"math/big"
	"github.com/ethereum/go-ethereum/common"
	"regexp"
	"encoding/hex"
	"strings"
	"strconv"
	"net/url"
)

type NodeID [64]byte

type Node struct {
	IP net.IP
	UDP, TCP uint16
	ID NodeID
	nodeNetGuts
}

func NewNode(id NodeID, ip net.IP, udpPort, tcpPort uint16) *Node{
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	}
	return &Node{
		IP:ip,
		UDP:udpPort,
		TCP:tcpPort,
		ID:id,
	}
}

func PubkeyID(pub *ecdsa.PublicKey) NodeID {
	var id NodeID
	pbytes := elliptic.Marshal(pub.Curve, pub.X, pub.Y)
	if len(pbytes)-1 != len(id) {
		panic(fmt.Errorf("need %d bit pubkey, got %d bits", (len(id)+1)*8, len(pbytes)))
	}
	copy(id[:], pbytes[1:])
	return id
}

func (n *Node)addr() *net.UDPAddr {
	return &net.UDPAddr{IP:n.IP, Port:int(n.UDP)}
}

func (n *Node) validateComplete() error {
	if n.Incomplete() {
		return errors.New("incomplete node")
	}
	if n.UDP == 0 {
		return errors.New("missing UDP port")
	}
	if n.TCP == 0 {
		return errors.New("missing TCP port")
	}
	if n.IP.IsMulticast() || n.IP.IsUnspecified() {
		return errors.New("invalid IP (multicast/unspecified)")
	}
	_, err := n.ID.Pubkey() // validate the key (on curve, etc.)
	return err
}

func (n *Node) Incomplete() bool {
	return n.IP == nil
}

func (n NodeID) Pubkey() (*ecdsa.PublicKey, error) {
	p := &ecdsa.PublicKey{Curve: crypto.S256(), X: new(big.Int), Y: new(big.Int)}
	half := len(n) / 2
	p.X.SetBytes(n[:half])
	p.Y.SetBytes(n[half:])
	if !p.Curve.IsOnCurve(p.X, p.Y) {
		return nil, errors.New("id is invalid secp256k1 curve point")
	}
	return p, nil
}

func distcmp(target, a, b common.Hash) int {
	for i := range target {
		da := a[i] ^ target[i]
		db := b[i] ^ target[i]
		if da > db {
			return 1
		} else if da < db {
			return -1
		}
	}
	return 0
}

func (n NodeID) String() string {
	return fmt.Sprintf("%x", n[:])
}

func MustParseNode(rawurl string) *Node {
	n, err := ParseNode(rawurl)
	if err != nil {
		panic("invalid node URL: " + err.Error())
	}
	return n
}

func ParseNode(rawurl string) (*Node, error) {
	if m := incompleteNodeURL.FindStringSubmatch(rawurl); m != nil {
		id, err := HexID(m[1])
		if err != nil {
			return nil, fmt.Errorf("invalid node ID (%v)", err)
		}
		return NewNode(id, nil, 0, 0), nil
	}
	return parseComplete(rawurl)
}

func parseComplete(rawurl string) (*Node, error) {
	var (
		id               NodeID
		ip               net.IP
		tcpPort, udpPort uint64
	)
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "enode" {
		return nil, errors.New("invalid URL scheme, want \"enode\"")
	}
	// Parse the Node ID from the user portion.
	if u.User == nil {
		return nil, errors.New("does not contain node ID")
	}
	if id, err = HexID(u.User.String()); err != nil {
		return nil, fmt.Errorf("invalid node ID (%v)", err)
	}
	// Parse the IP address.
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		return nil, fmt.Errorf("invalid host: %v", err)
	}
	if ip = net.ParseIP(host); ip == nil {
		return nil, errors.New("invalid IP address")
	}
	// Ensure the IP is 4 bytes long for IPv4 addresses.
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	}
	// Parse the port numbers.
	if tcpPort, err = strconv.ParseUint(port, 10, 16); err != nil {
		return nil, errors.New("invalid port")
	}
	udpPort = tcpPort
	qv := u.Query()
	if qv.Get("discport") != "" {
		udpPort, err = strconv.ParseUint(qv.Get("discport"), 10, 16)
		if err != nil {
			return nil, errors.New("invalid discport in query")
		}
	}
	return NewNode(id, ip, uint16(udpPort), uint16(tcpPort)), nil
}

var incompleteNodeURL = regexp.MustCompile("(?i)^(?:enode://)?([0-9a-f]+)$")

func HexID(in string) (NodeID, error) {
	var id NodeID
	b, err := hex.DecodeString(strings.TrimPrefix(in, "0x"))
	if err != nil {
		return id, err
	} else if len(b) != len(id) {
		return id, fmt.Errorf("wrong length, want %d hex chars", len(id)*2)
	}
	copy(id[:], b)
	return id, nil
}