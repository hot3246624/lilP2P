package main

import (
	"net/http"
	"golang.org/x/net/websocket"
	"log"
	"fmt"
)

func main(){
	//service := ":1234"
	//tcpAddr, err := net.ResolveTCPAddr("tcp", service)
	//utils.CheckError(err)
	//listener, err := net.ListenTCP("tcp", tcpAddr)
	//utils.CheckError(err)
	//
	//for {
	//	conn, err := listener.AcceptTCP()
	//	if err != nil{
	//		continue //为了维护服务器的运行，在这里不处理错误。
	//	}
	//	utils.CheckError(err)
	//	go handleClient(conn)
	//}

	http.Handle("/", websocket.Handler(Echo))
	if err := http.ListenAndServe(":1234", nil); err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}


func Echo(ws *websocket.Conn) {
	var err error
	for {
		var reply string
		if err = websocket.Message.Receive(ws, &reply); err != nil {
			fmt.Println("Can't receive")
			break
		}

		fmt.Println("Received back from client: " + reply)
		msg := "Received: " + reply
		fmt.Println("Sending to client: " + msg)
		if err = websocket.Message.Send(ws, msg); err != nil {
			fmt.Println("Can't send")
			break
		}
	}
}
