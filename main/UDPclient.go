package main

import (
	"os"
	"fmt"
	"net"
	"studyP2P/utils"
	"time"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s host:port", os.Args[0])
		os.Exit(1)
	}
	service := os.Args[1]
	udpAddr, err := net.ResolveUDPAddr("udp4", service)
	utils.CheckError(err)
	conn, err := net.DialUDP("udp", nil, udpAddr)
	utils.CheckError(err)

	_, err = conn.Write([]byte("anything"))
	utils.CheckError(err)
	var buf [512]byte
	n, err := conn.Read(buf[0:])
	utils.CheckError(err)
	fmt.Println(string(buf[0:n]))
	//os.Exit(0)

	for {
		fmt.Println(Ping(conn))
		time.Sleep(10*time.Second)
	}
}


func Ping(conn *net.UDPConn) string {
	_, err := conn.Write([]byte("ping"))
	utils.CheckError(err)
	var buf [512]byte
	n, err := conn.Read(buf[0:])
	return string(buf[0:n])
}
