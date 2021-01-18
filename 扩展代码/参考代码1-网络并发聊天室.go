package main

import (
	"net"
	"fmt"
	"strings"
	"time"
)

// 定义用户结构体类型
type Client struct {
	C    chan string
	Name string
	Addr string
}

// 定义全局 map 存储在线用户 key:IP+port, value:Client
var onlineMap map[string]Client

// 定义全局 channel 处理消息
var message = make(chan string)

func WriteMsgToClient(clnt Client, conn net.Conn) {
	// 循环跟踪 clnt.C，有消息则读走，Write 给客户端
	for msg := range clnt.C {
		conn.Write([]byte(msg + "\n")) // 发送消息 给客户端
	}
}

func MakeMsg(clnt Client, msg string) (buf string) {
	buf = "[" + clnt.Addr + "]" + clnt.Name + ": " + msg
	return
}

func HandleConn(conn net.Conn) {
	defer conn.Close()
	// 获取新连接上来的用户的网络地址(IP+port)
	netAddr := conn.RemoteAddr().String()
	// 给新用户创建结构体。用户名、网络地址一样
	clnt := Client{make(chan string), netAddr, netAddr}
	// 将新创建的结构体，添加到 map 中,key值为获取到的网络地址(IP+port)
	onlineMap[netAddr] = clnt

	// 新创建一个协程，专门给当前客户端发送消息。
	go WriteMsgToClient(clnt, conn)

	// 广播新用户上线
	//message <- "[" + clnt.Addr + "]" + clnt.Name + ": login"
	message <- MakeMsg(clnt, "login")

	// 检测用户主动退出
	isQuit := make(chan bool)
	
	// 检测用户是否有消息发送
	hasData := make(chan bool)

	// 创建一个新go程，循环读取用户发送的消息，广播给在线用户
	go func() {
		// 定义切片缓冲区，存储读到的用户消息
		buf := make([]byte, 2048)
		for {
			n, err := conn.Read(buf)
			if n == 0 {
				isQuit <- true		// 用户主动退出登录
				fmt.Printf("用户%s退出登录\n", clnt.Name)
				return
			}
			if err != nil {
				fmt.Println("Read err:", err)
				return
			}

			msg := string(buf[:n-1]) 		// 保存用户写来的消息内容, nc 工具默认添加‘\n’
			if msg == "who" && len(msg) == 3 { 			// 判断用户发送了 who 指令
				conn.Write([]byte("user list:\n"))
				for _, user := range onlineMap { 		// 遍历map获取在线用户
					userInfo := user.Addr + ":" + user.Name + "\n" // 组织在线用户信息
					conn.Write([]byte(userInfo))                   // 写给当前用户
				}
				
			// 判断用户输入的前6个字符是否为 rename
			} else if len(msg) >= 8 && msg[:6] == "rename" { 	// rename|Iron man
				newName := strings.Split(msg, "|")[1] 			// 按"|"拆分，rename为[0], Iron man 为[1]
				clnt.Name = newName                   // 替换掉当期用户原始Name
				onlineMap[netAddr] = clnt             // 使用netAddr为key找到map中当前用户。覆盖
				conn.Write([]byte("rename successful\n"))
				
				
			} else {
				message <- MakeMsg(clnt, msg) 		// 将消息广播给所有在线用户
			}
			hasData <- true			// 只要执行到，就说明用户有消息发送
		}
	}()

	for { // 不能让当前go程结束。
		select {
			case <-isQuit:									// 用户不主动退出，阻塞
				delete(onlineMap, netAddr)					// 将当前用户从map中移除
				message <- MakeMsg(clnt, "logout")	// 广播给在线用户，谁退出了
				return 										// 结束当前退出用户对应go程
			case <-hasData:
															// 什么都不做，目的是让计时器归零
			case <-time.After(10*time.Second):
				delete(onlineMap, netAddr)					// 将当前用户从map中移除
				message <- MakeMsg(clnt, "time out leave")	// 广播给在线用户，超时退出
				return 										// 结束当前退出用户对应go程
		}
	}
}

func Manager() {
	// 给map分配空间
	onlineMap = make(map[string]Client)

	// 循环读取 message 通道中的数据
	for {
		// 通道 message 中有数据读到 msg 中。 没有，则阻塞
		msg := <-message

		// 一旦执行到这里，说明message中有数据了，解除阻塞。 遍历 map
		for _, clnt := range onlineMap {
			clnt.C <- msg // 把从Message通道中读到的数据，写到 client 的 C 通道中。
		}
	}
}

func main() {
	// 创建监听 socket
	listener, err := net.Listen("tcp", "127.0.0.1: 8000")
	if err != nil {
		fmt.Println("Listen err:", err)
		return
	}
	defer listener.Close()

	// 创建go程 处理消息
	go Manager()

	// 循环接收客户端连接请求
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Accept err:", err)
			continue // 失败，监听其他客户端连接
		}
		defer conn.Close()

		// 给新连接的客户端，单独创建一个go程，处理客户端连接请求
		go HandleConn(conn)
	}
}
