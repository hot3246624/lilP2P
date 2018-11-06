package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"studyP2P/utils"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s host:port ", os.Args[0])
		os.Exit(1)
	}
	service := os.Args[1]
	tcpAddr, err := net.ResolveTCPAddr("tcp4", service)
	utils.CheckError(err)
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	utils.CheckError(err)
	_, err = conn.Write([]byte("HEAD / HTTP/1.0\r\n\r\n"))
	utils.CheckError(err)
	result, err := ioutil.ReadAll(conn)
	utils.CheckError(err)
	fmt.Println(string(result))
	os.Exit(0)
}
