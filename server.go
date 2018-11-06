package studyP2P

import (
	"crypto/ecdsa"
	"studyP2P/discovery"

	"sync"
	"net"
	"time"
	"errors"
	"studyP2P/netutil"
	"fmt"
)

const (
	defaultMaxPendingPeers = 50
)

type connFlag int32

const (
	dynDialedConn connFlag = 1 << iota
	staticDialedConn
	inboundConn
	trustedConn
)

type conn struct {
	fd net.Conn
	transport
	node  *discovery.Node
	flags connFlag
	cont  chan error // The run loop uses cont to signal errors to SetupConn.
	//caps  []Cap      // valid after the protocol handshake
	//name  string     // valid after the protocol handshake
}

type transport interface {
	// The two handshakes.
	doEncHandshake(prv *ecdsa.PrivateKey, dialDest *ecdsa.PublicKey) (*ecdsa.PublicKey, error)
	//doProtoHandshake(our *protoHandshake) (*protoHandshake, error)
	// The MsgReadWriter can only be used after the encryption
	// handshake has completed. The code uses conn.id to track this
	// by setting it to a non-nil value after the encryption handshake.
	//MsgReadWriter
	// transports must provide Close because we use MsgPipe in some of
	// the tests. Closing the actual network connection doesn't do
	// anything in those tests because NsgPipe doesn't use it.
	close(err error)
}

type Config struct {
	// This field must be set to a valid secp256k1 private key.
	PrivateKey *ecdsa.PrivateKey `toml:"-"`
	// MaxPeers is the maximum number of peers that can be
	// connected. It must be greater than zero.
	MaxPeers int
	// MaxPendingPeers is the maximum number of peers that can be pending in the
	// handshake phase, counted separately for inbound and outbound connections.
	// Zero defaults to preset values.
	MaxPendingPeers int `toml:",omitempty"`
	// DialRatio controls the ratio of inbound to dialed connections.
	// Example: a DialRatio of 2 allows 1/2 of connections to be dialed.
	// Setting DialRatio to zero defaults it to 3.
	DialRatio int `toml:",omitempty"`
	// NoDiscovery can be used to disable the peer discovery mechanism.
	// Disabling is useful for protocol debugging (manual topology).
	NoDiscovery bool
	// Discovery specifies whether the new topic-discovery based V5 discovery
	// protocol should be started or not.
	Discovery bool `toml:",omitempty"`
	// Name sets the node name of this server.
	// Use common.MakeName to create a name that follows existing conventions.
	Name string `toml:"-"`
	//// BootstrapNodes are used to establish connectivity
	//// with the rest of the network.
	//BootstrapNodes []*discovery.Node

	// BootstrapNodesV5 are used to establish connectivity
	// with the rest of the network using the V5 discovery
	// protocol.
	BootstrapNodes []*discovery.Node `toml:",omitempty"`
	// Static nodes are used as pre-configured connections which are always
	// maintained and re-connected on disconnects.
	StaticNodes []*discovery.Node
	// Trusted nodes are used as pre-configured connections which are always
	// allowed to connect, even above the peer limit.
	TrustedNodes []*discovery.Node

	// Connectivity can be restricted to certain IP networks.
	// If this option is set to a non-nil value, only hosts which match one of the
	// IP networks contained in the list are considered.
	//NetRestrict *netutil.Netlist `toml:",omitempty"`

	// NodeDatabase is the path to the database containing the previously seen
	// live nodes in the network.
	NodeDatabase string `toml:",omitempty"`

	// Protocols should contain the protocols supported
	// by the server. Matching protocols are launched for
	// each peer.
	//Protocols []Protocol `toml:"-"`

	// If ListenAddr is set to a non-nil address, the server
	// will listen for incoming connections.
	//
	// If the port is zero, the operating system will pick a port. The
	// ListenAddr field will be updated with the actual address when
	// the server is started.
	ListenAddr string

	// If set to a non-nil value, the given NAT port mapper
	// is used to make the listening port available to the
	// Internet.
	//NAT nat.Interface `toml:",omitempty"`

	// If Dialer is set to a non-nil value, the given Dialer
	// is used to dial outbound peer connections.
	//Dialer NodeDialer `toml:"-"`

	// If NoDial is true, the server will not dial any peers.
	NoDial bool `toml:",omitempty"`
	// If EnableMsgEvents is set then the server will emit PeerEvents
	// whenever a message is sent to or received from a peer
	EnableMsgEvents bool
	// Logger is a custom logger to use with the p2p.Server.
	//Logger log.Logger `toml:",omitempty"`
}

type Server struct {
	Config

	lock sync.Mutex

	running bool

	//localnode

	ntab discoveryTable

	listener net.Listener

	lastLookup time.Time

	DiscNetwork *discovery.Network

	//peerOp chan peerOpFunc

	//peerOpDone chan struct{}

	quit chan struct{}

	addstatic     chan *discovery.Node
	removestatic  chan *discovery.Node
	addtrusted    chan *discovery.Node
	removetrusted chan *discovery.Node
	//posthandshake chan *conn
	//addpeer       chan *conn
	//delpeer       chan peerDrop
	loopWG        sync.WaitGroup // loop, listenLoop
	//peerFeed      event.Feed
	//log           log.Logger
}

func (srv *Server)Start() (err error) {
	srv.lock.Lock()
	defer srv.lock.Unlock()
	if srv.running {
		return errors.New("server alreay running.")
	}
	srv.running = true
	//if srv.NoDial && srv.ListenAddr == ""{
	//	fmt.Println("P2P server will be useless, neither dialing nor listening")
	//}

	if srv.PrivateKey == nil {
		return fmt.Errorf("Server.PrivateKey must be set to a non-nil key")
	}

	srv.quit = make(chan struct{})
	//srv.addpeer = make(chan *conn)
	//srv.delpeer = make(chan peerDrop)
	//srv.posthandshake = make(chan *conn)
	srv.addstatic = make(chan *discovery.Node)
	srv.removestatic = make(chan *discovery.Node)
	srv.addtrusted = make(chan *discovery.Node)
	srv.removetrusted = make(chan *discovery.Node)
	//srv.peerOp = make(chan peerOpFunc)
	//srv.peerOpDone = make(chan struct{})
	//if err := srv.setupLocalNode(); err != nil {
	//	return err
	//}
	//TCP处理
	if srv.ListenAddr != ""{
		//if err := srv.setupListening(); err != nil {
		//	return err
		//}
	}

	//UDP处理-discovery
	if err := srv.setupDiscovery(); err != nil {
		return nil
	}
	return nil
}

func (srv *Server)setupListening() error {
	listen, err := net.Listen("tcp", srv.ListenAddr)
	if err != nil {
		return err
	}
	laddr := listen.Addr().(*net.TCPAddr)
	srv.ListenAddr = laddr.String()
	srv.listener = listen

	srv.loopWG.Add(1)
	go srv.listenLoop()
	return nil
}

//TCP监听和处理
func (srv *Server)listenLoop() {
	defer srv.loopWG.Done()
	tokens := defaultMaxPendingPeers
	if srv.MaxPendingPeers > 0 {
		tokens = srv.MaxPendingPeers
	}

	slots := make(chan struct{}, tokens)
	for i := 0; i < tokens; i++ {
		slots <- struct{}{}
	}
	for{
		<- slots
		var (
			fd net.Conn
			err error
		)
		for {
			fd, err = srv.listener.Accept()
			if netutil.IsTemporaryError(err){
				continue
			}else if err != nil {
				return
			}
			break
		}
		//var ip net.IP
		//if tcp, ok := fd.RemoteAddr().(*net.TCPAddr); ok {
		//	ip = tcp.IP
		//}
		go func() {
			srv.SetupConn(fd, inboundConn, nil)
			slots <- struct{}{}
		}()

	}
}

//type conn struct {
//	fd net.Conn
//	transport
//	node  *enode.Node
//	flags connFlag
//	cont  chan error // The run loop uses cont to signal errors to SetupConn.
//	caps  []Cap      // valid after the protocol handshake
//	name  string     // valid after the protocol handshake
//}

//type transport interface {
//	// The two handshakes.
//	doEncHandshake(prv *ecdsa.PrivateKey, dialDest *ecdsa.PublicKey) (*ecdsa.PublicKey, error)
//	doProtoHandshake(our *protoHandshake) (*protoHandshake, error)
//	// The MsgReadWriter can only be used after the encryption
//	// handshake has completed. The code uses conn.id to track this
//	// by setting it to a non-nil value after the encryption handshake.
//	MsgReadWriter
//	// transports must provide Close because we use MsgPipe in some of
//	// the tests. Closing the actual network connection doesn't do
//	// anything in those tests because NsgPipe doesn't use it.
//	close(err error)
//}

func (srv *Server)SetupConn(fd net.Conn, flags connFlag, dialDest *discovery.Node) error {
	//c := &conn{}
	err := srv.setupConn(fd, flags, dialDest)
	if err != nil {
		fd.Close()
	}
	return err
}

func (srv *Server)setupConn(c net.Conn, flags connFlag, dialDest *discovery.Node) error {
	srv.lock.Lock()
	running := srv.running
	srv.lock.Unlock()
	if !running {
		return errors.New("serverStopped")
	}
	//var dialPubKey *ecdsa.PublicKey
	//if dialDest != nil {
	//	dialPubKey = new(ecdsa.PublicKey)
	//	if err := dialDest.
	//}
	//TODO 两次握手，将conn发送到通道之中
	return nil
}

func (srv *Server) checkpoint(c *conn, stage chan<- *conn) error {
	select {
	case stage <- c:
	case <-srv.quit:
		return errors.New("errServerStopped")
	}
	select {
	case err := <-c.cont:
		return err
	case <-srv.quit:
		return errors.New("errServerStopped")
	}
}

func (srv *Server)setupDiscovery() error{
	if srv.NoDiscovery && !srv.Discovery{
		return nil
	}

	addr, err := net.ResolveUDPAddr("udp", srv.ListenAddr)
	if err != nil {
		return err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}

	realaddr := conn.LocalAddr().(*net.UDPAddr)
	fmt.Println("UDP listener up", "addr", realaddr)
	//unhandled := make(chan discovery.ReadPacket, 100)
	//sconn := &sharedUDPConn{conn, unhandled}

	var ntab *discovery.Network
	unhandled := make(chan discovery.ReadPacket, 100)
	sconn := &sharedUDPConn{conn, unhandled}
	if sconn != nil {
		ntab, err = discovery.ListenUDP(srv.PrivateKey, sconn, "")
	} else {
		ntab, err = discovery.ListenUDP(srv.PrivateKey, conn, "")
	}
	if err != nil {
		return err
	}

	if err := ntab.SetFallbackNodes(srv.BootstrapNodes); err != nil {
		return err
	}

	srv.DiscNetwork = ntab
	return nil
}

func (srv *Server) Stop() {
	srv.lock.Lock()
	if !srv.running {
		srv.lock.Unlock()
		return
	}
	srv.running = false
	if srv.listener != nil {
		// this unblocks listener Accept
		srv.listener.Close()
	}
	close(srv.quit)
	srv.lock.Unlock()
	srv.loopWG.Wait()
}

type sharedUDPConn struct {
	*net.UDPConn
	unhandled chan discovery.ReadPacket
}