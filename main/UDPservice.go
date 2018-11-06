package main

import (
	"net"
	"studyP2P/utils"
	//"time"
)

func main() {
	service := ":1200"
	udpAddr, err := net.ResolveUDPAddr("udp4", service)
	utils.CheckError(err)
	conn, err := net.ListenUDP("udp", udpAddr)
	utils.CheckError(err)
	for {
		handleClient(conn)
	}
}
func handleClient(conn *net.UDPConn) {
	var buf [512]byte
	_, addr, err := conn.ReadFromUDP(buf[0:])
	if err != nil {
		return
	}
	//daytime := time.Now().String()


	conn.WriteToUDP([]byte("pong"), addr)
}