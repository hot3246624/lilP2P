package main

import (
	"crypto/ecdsa"
	"log"
	"studyP2P"
	"github.com/ethereum/go-ethereum/crypto"
	"fmt"
	"time"
	"flag"
	"studyP2P/discovery"
	"studyP2P/utils"
	"encoding/hex"
)

func startServer(remoteKey *ecdsa.PrivateKey, port int) *studyP2P.Server {
	config := studyP2P.Config{
		Name:       "test",
		MaxPeers:   10,
		ListenAddr: fmt.Sprintf("127.0.0.1:%v", port),
		PrivateKey: remoteKey,
		Discovery: true,
		BootstrapNodes: BootstrapNodes(),
	}
	server := &studyP2P.Server{
		Config:       config,

		//newPeerHook:  pf,
		//newTransport: func(fd net.Conn) transport { return newTestTransport(remoteKey, fd) },
	}
	if err := server.Start(); err != nil {
		log.Fatal("Could not start server: %v", err)
	}
	return server
}

func main() {
	listenF := flag.Int("l", 0, "wait for incoming connections")
	bswitch := flag.Bool("s", false, "start bootstrap")
	index := flag.Int("i", 0, "start bootstrap")

	//target := flag.String("d", "", "target peer to dial")
	//seed := flag.Int64("seed", 0, "set random seed for id generation")
	flag.Parse()

	// start the test server
	var priKey *ecdsa.PrivateKey
	var priKeyTemp []byte
	priKey = newkey()
	//	priKey2 := hex.EncodeToString(crypto.FromECDSA(priKey))
	//	priKey3, _ := hex.DecodeString(priKey2)
	//	priKey4, _ := crypto.ToECDSA(priKey3)
	//	fmt.Println(priKey, priKey4)
	//	ID := discovery.PubkeyID(&priKey.PublicKey)
	if *bswitch {
		if *index == 1 {
			priKeyTemp, _ = hex.DecodeString("793c4f6c8582df7173f3f92d0077147635d790018aa3b8b36ea13e46aa7c2030")
			*listenF = 30303
		}else if *index == 2{
			priKeyTemp, _ = hex.DecodeString("8466d367aafc737cf108402d9db1c3ead491bc9625e1c835cb5132ade39c6c82")
			*listenF = 30304
		}else if *index == 3{
			priKeyTemp, _ = hex.DecodeString("620f112c18ab93aa90a3459aa27d91ccbd327f1c75ee220a6ba96ca6331e7e1d")
			*listenF = 30305
		}else if *index == 4{
			priKeyTemp, _ = hex.DecodeString("843aca89c3c51647c4ece344b4b0845f6a57c14ded484b4bf62b9df943d368cc")
			*listenF = 30306
		}else if *index == 5{
			priKeyTemp, _ = hex.DecodeString("8f0dd3f9478f88d208b91411b6e63a0e7990165a21d51dba9fd7a5acd6b21a7a")
			*listenF = 30307
		}
		priKey, _ = crypto.ToECDSA(priKeyTemp)
	}
	fmt.Println("P2P start form now on:", time.Now())
	fmt.Println("our listen port is:", *listenF)
	srv := startServer(priKey, *listenF)
	defer srv.Stop()
	for {
		time.Sleep(30*time.Second)
		fmt.Println(time.Now())
	}
	//connected := make(chan *Peer)
	//remid := &newkey().PublicKey
	//srv := startTestServer(t, remid, func(p *Peer) {
	//	if p.ID() != enode.PubkeyToIDV4(remid) {
	//		t.Error("peer func called with wrong node id")
	//	}
	//	connected <- p
	//})
	//defer close(connected)
	//defer srv.Stop()

	// dial the test server
	//conn, err := net.DialTimeout("tcp", srv.ListenAddr, 5*time.Second)
	//if err != nil {
	//	t.Fatalf("could not dial: %v", err)
	//}
	//defer conn.Close()
	//
	//select {
	//case peer := <-connected:
	//	if peer.LocalAddr().String() != conn.RemoteAddr().String() {
	//		t.Errorf("peer started with wrong conn: got %v, want %v",
	//			peer.LocalAddr(), conn.RemoteAddr())
	//	}
	//	peers := srv.Peers()
	//	if !reflect.DeepEqual(peers, []*Peer{peer}) {
	//		t.Errorf("Peers mismatch: got %v, want %v", peers, []*Peer{peer})
	//	}
	//case <-time.After(1 * time.Second):
	//	t.Error("server did not accept within one second")
	//}
}

func newkey() *ecdsa.PrivateKey {
	key, err := crypto.GenerateKey()
	if err != nil {
		panic("couldn't generate key: " + err.Error())
	}
	return key
}

func BootstrapNodes() (nodes []*discovery.Node) {
	nodes = utils.FoundationBootnodes()
	return
}
