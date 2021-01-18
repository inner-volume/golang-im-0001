package main

import (
	"net"
	"fmt"
	"runtime"
	"sync"
	"io"
	"strings"
)

// 创建全局读写锁, 包含公共区数据
var rwMutex sync.RWMutex

// 创建全局的client结构体
type Client struct {
	Name string // 初始的 name == Addr
	Addr string
	C    chan string
}

// 创建全局在线用户列表 -- map
var onlineMap = make(map[string]Client)

// 创建全局message
var message = make(chan string)

func WriteMsgToClient(clt Client, conn net.Conn) {
	for {
		msg := <-clt.C // 无数据, 阻塞; 有数据, 继续
		// 将读到的数据写给 对应的客户端
		conn.Write([]byte(msg + "\n"))
	}
}

// 负责对接客户端数据通信
func handleConnect(conn net.Conn) {

	defer conn.Close()

	// 获取客户端地址结构
	cltAddr := conn.RemoteAddr().String()

	// 初始化客户端
	clt := Client{cltAddr, cltAddr, make(chan string)}

	// 添加在线用户之前,加写锁
	rwMutex.Lock()
	// 添加到在线用户列表中
	onlineMap[cltAddr] = clt
	// 添加完成,立即解锁
	rwMutex.Unlock()

	// 启动 go程 读用户自己的 耳朵(channel -- C)中的数据
	go WriteMsgToClient(clt, conn)

	// 组织用户上线广播消息　［IP+port］Name　: 上线！
	//msg := "[" + cltAddr + "]" + clt.Name + ": " + "上线！"
	msg := makeMsg("上线!", clt)

	// 将上线消息，写入全局　ｍｅｓｓａｇｅ　内, 自动完成广播.
	message <- msg

	// 在用户广播自己上线后,创建匿名go程, 获取用户聊天内容.
	go func() {
		buf := make([]byte, 4096)

		// 循环, 读取用户发送的消息.
		for {
			n, err := conn.Read(buf)	// 无数据, 阻塞; 有数据, 继续
			if n == 0 {
				fmt.Println("客户端下线!")
				return
			}
			if err != nil && err != io.EOF {
				fmt.Println("Read err:", err)
				return
			}
			//fmt.Println("测试---:", buf[:n])
			msg := string(buf[:n-1])

			// 判断,用户是否发送的是一个查询在线用户命令
			if "who" == msg {

				// 实时遍历在线用户列表
				rwMutex.RLock() 	// 在遍历在线用户列表之前,加读锁
				// 实时的遍历在线用户列表
				for _, clt := range onlineMap {
					// 将在线用户,组织成消息,
					onLineMsg := makeMsg("[在线]!\n", clt)
					// 发送给当前用户
					conn.Write([]byte(onLineMsg))
				}
				rwMutex.RUnlock()  // 遍历结束后,立即解锁

			} else if len(msg) >= 8 && msg[:6] == "rename" { // rename | Iron man
				newName := strings.Split(msg, "|")[1] // 按"|"拆分，rename为[0], Iron man为[1]
				clt.Name = newName                   // 替换掉当前用户原始Name
				onlineMap[cltAddr] = clt             // 使用netAddr为key找到map中当前用户。覆盖
				conn.Write([]byte("rename successful\n"))

			} else {

				// 组织用户聊天的内容, 并写入 全局 message , 广播!!!
				message <- makeMsg(msg, clt)
			}
		}
	}()

	// 添加测试代码,防止当前go程提前退出.
	for {
		runtime.GC()
	}
}

// 封装函数, 组织发送的数据信息
func makeMsg(str string, clt Client) string {

	return "[" + clt.Addr + "]" + clt.Name + ": " + str
}

// 全局的 Manager go程, 监听全局message 中是否有数据
func Manager() {
	for {
		msg := <-message // 无数据,阻塞; 有数据,继续

		rwMutex.RLock() 	// 在遍历在线用户列表之前,加读锁
		// 实时的遍历在线用户列表
		for _, clt := range onlineMap {
			// 将全局 message 中读到的数据, 写给用户自己的耳朵中(channel -- C)
			clt.C <- msg
		}
		rwMutex.RUnlock()  // 遍历结束后,立即解锁
	}
}

func main() {
	// 启动服务器, 监听客户端的链接请求
	listener, err := net.Listen("tcp", "127.0.0.1:8800")
	if err != nil {
		fmt.Println("listen err:", err)
		return
	}
	defer listener.Close()

	// 创建并启动 Manager管理者go程.
	go Manager()

	// 服务器循环监听客户端链接请求
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Accept err:", err)
			continue
		}
		// 启动新go程,与客户端对接数据通信
		go handleConnect(conn)
	}
}
