package main

import (
	"net"
	"fmt"
	//"runtime"
	"sync"
	"io"
	"strings"
	"time"
)

/*
私聊演示：
	1. 登录的客户端，需先使用 rename| 改名。 如： AAA BBB CCC 
	
	2. 测试1：AAA@BBB:你好，在吗？
	
	3. 此时，BBB用户收到：“AAA用户对你说：你好，在吗？” 。 CCC 用户无消息接收。
	
	4. 测试2：AAA@DDD:上线了吗？   
	
	5. 此时，AAA用户收到：“对不起，你私聊的用户不在线”
	
	6. 如不使用@发消息，则判定为 公聊。
*/

// 创建 用户结构体
type Client struct {
	Name string
	Addr string
	C    chan string
}

// 创建全局读写锁 -- 保护在线用户列表
var rwMutex sync.RWMutex

// 创建 全局 channel
var Message = make(chan string)

// 创建全局在线用户列表 onlineMap   key:string   value:Client
var onlineMap = make(map[string]Client)

func main() {
	// 创建监听器
	listener, err := net.Listen("tcp", "127.0.0.1:8888")
	if err != nil {
		fmt.Println("net.Listen err:", err)
		return
	}
	defer listener.Close()

	// 启动 Manager go程 监听全局 channel
	go Manager()

	// for 监听客户端链接请求
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("listener.Accept err:", err)
			continue
		}

		// 启动 go程  handleConnect
		go handleConnect(conn)
	}
}

// 创建 管理者go程, 监听全局channel, 遍历在线用户列表
func Manager() {

	for {
		msg := <-Message // 无数据,阻塞; 有数据,写入所有在线用户自带channel.

		retStr1 := strings.Split(msg, "@")

		if len (retStr1) > 1 {		// 私聊
			// 获取src 用户名
			srcName := retStr1[0]			// AAA
			// 获取det 用户名 和 消息
			retStr2 := strings.Split(retStr1[1], ":")

			dstName := retStr2[0]			// BBB

			privateMsg := retStr2[1]		// hello

			var tmpClt Client

			var flg = true

			rwMutex.RLock()
			// 遍历在线用户列表, 匹配用户名, 写给当前用户
			for _, clt := range onlineMap {
				// 如果目标用户在 列表中, 发送消息给此用户
				if clt.Name == dstName {
					clt.C <- srcName + "对你说:" + privateMsg
					flg = false
					break
				}
				if clt.Name == srcName {
					tmpClt = clt
				}
			}
			rwMutex.RUnlock()

			if flg {
				tmpClt.C <- "对不起!你私聊的用户不在线!"
			}

		} else {		// 公聊
			// 加 读锁
			rwMutex.RLock()
			for _, clt := range onlineMap {
				clt.C <- msg
			}
			rwMutex.RUnlock()
		}
	}
}

// 创建 监听用户自带channel 的 go程
func WriteMsgToClient(clt Client, conn net.Conn) {
	/*	for {
			msg := <- clt.C	// 无数据,阻塞; 有数据,写给 当前用户.
			conn.Write([]byte(msg + "\n"))
		}*/
	for msg := range clt.C {
		conn.Write([]byte(msg + "\n"))
	}
}

// 实现 handleConnect
func handleConnect(conn net.Conn) {

	defer conn.Close()

	// 获取客户端地址信息 - conn.Remote
	cltAddr := conn.RemoteAddr().String()

	// 初始化新用户信息  Name == IP+port
	clt := Client{cltAddr, cltAddr, make(chan string)}

	// 加写锁
	rwMutex.Lock()
	// 写入全局在线用户列表 onlineMap
	onlineMap[cltAddr] = clt
	rwMutex.Unlock()

	// 启动 go程 WriteMsgToClient 监听用户自带 channel
	go WriteMsgToClient(clt, conn)

	// 组织上线用户信息.
	//loginMsg := "[" + clt.Addr + "]" + clt.Name + ":" + "上线!"
	loginMsg := makeMsg(clt, "上线!")

	// 写给 全局 Message
	Message <- loginMsg

	// 创建监听用户退出的 channel
	isQuit := make(chan bool)

	// 创建监听用户超时的 channel,
	isLive := make(chan bool)

	// 创建新 go程,获取用户输入
	go func() {
		buf := make([]byte, 4096)

		for {
			n, err := conn.Read(buf)
			if n == 0 {
				//fmt.Println("[", cltAddr, "]", "客户端下线!")
				// 清理用户相关资源
				isQuit <- true

				return // Goexit()
			}
			if err != nil && err != io.EOF {
				fmt.Println("conn.Read err:", err)
				return
			}
			// 提取用户消息(去除'\n')
			msg := string(buf[:n-1])

			// 判断查询在线用户操作
			if msg == "who" {
				rwMutex.RLock()
				// 遍历在线用户列表
				for _, clt := range onlineMap {
					// 组织在线用户信息
					onlineMsg := makeMsg(clt, "[在线]\n")
					// 写给当前用户自己,不广播!
					conn.Write([]byte(onlineMsg))
				}
				rwMutex.RUnlock()

				// 判断用户执行了一个改名操作
			} else if len(msg) > 7 && msg[:7] == "rename|" {
				// 拆分用户输入的 新用户名
				newName := strings.Split(msg, "|")[1]
				// 更新当前用户, 变为含有新用户名的新用户
				clt.Name = newName

				rwMutex.Lock()
				// 将新用户,更新到在线用户列表中
				onlineMap[clt.Addr] = clt
				rwMutex.Unlock()

				// 通知当前用户改名成功
				conn.Write([]byte("rename successfull!!!\n"))

			} else {
				// -- 以@为判断依据, 区分 "私聊" / "公聊"
				if strings.Contains(msg, "@") {  // 私聊

					Message <- msg  // 直接写入 全局 Message

				} else {		// 公聊
					// 组织广播消息.
					msg = makeMsg(clt, msg)
					// 广播!
					Message <- msg
				}
			}
			// 用户的任意一个操作,都代表当前用户为 活跃用户.
			isLive <-true
		}
	}()

	// 防止当前go程提前退出
	for {
		select {
		case <-isQuit:
			// 关闭 用户自带 C. --- WriteMsgToClient go程 自动结束.
			close(clt.C)

			rwMutex.Lock()
			// 从在线用户列表,删除当前用户
			delete(onlineMap, clt.Addr)
			rwMutex.Unlock()

			// 组织用户下线消息
			logoutMsg := makeMsg(clt, "--下线!")

			// 广播
			Message <- logoutMsg

			// 清理用户资源.
			return // Goexit()

		case <-isLive:
			// 什么也不做,存在意义就是重置下面的定时器.

		case <-time.After(time.Second * 3600):
			// 关闭 用户自带 C. --- WriteMsgToClient go程 自动结束.
			close(clt.C)

			rwMutex.Lock()
			// 从在线用户列表,删除当前用户
			delete(onlineMap, clt.Addr)
			rwMutex.Unlock()

			// 组织用户下线消息
			logoutMsg := makeMsg(clt, "--超时,被强踢下线!")

			// 广播
			Message <- logoutMsg

			// 清理用户资源.
			return // Goexit()
		}
	}
}

func makeMsg(clt Client, str string) string {
	msg := "[" + clt.Addr + "]" + clt.Name + ":" + str
	return msg
}
