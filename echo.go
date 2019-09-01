package main

import (
	"bytes"
	"fmt"
	"github.com/op/go-logging"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
)

var mu sync.Mutex
var connection map[string]int
var connectionCount int64
var log = logging.MustGetLogger("example")
var format = logging.MustStringFormatter(
	`[%{time:15:04:05.000}] %{shortfunc} ▶ %{level:.4s} %{message}`,
)

type Option struct {
	delay bool
	echo  bool
}

type Client struct {
	option *Option
	conn   *net.TCPConn
}

func init() {
	connection = make(map[string]int)
	// For demo purposes, create two backend for os.Stderr.
	file, _ := os.OpenFile("echo.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
	backend := logging.NewLogBackend(file, "", 0)

	// For messages written to backend2 we want to add some additional
	// information to the output, including the used log level and the name of
	// the function.
	backend2Formatter := logging.NewBackendFormatter(backend, format)

	// Only errors and more severe messages should be sent to backend1
	backendLeveled := logging.AddModuleLevel(backend)
	backendLeveled.SetLevel(logging.ERROR, "")

	// Set the backends to be used.
	logging.SetBackend(backendLeveled, backend2Formatter)
}

func main() {
	ln, err := net.Listen("tcp", ":8912")
	if err != nil {
		log.Fatalf("%v", err)
	}

	tcpln, ok := ln.(*net.TCPListener)
	if !ok {
		log.Error("ln is not tcp lisenter")
		return
	}

	for {
		conn, err := tcpln.AcceptTCP()
		conn.SetNoDelay(false)
		cli := &Client{
			option: &Option{
				delay: false,
				echo:  false,
			},
			conn: conn,
		}
		if err != nil {
			log.Errorf("accept error:%v", err)
		}
		go handleConnection(cli)
	}
}

func handleConnection(cli *Client) {
	conn := cli.conn
	option := cli.option
	defer conn.CloseWrite()

	var total int
	addr := conn.RemoteAddr()
	ip := getIP(addr.String())
	log.Infof("%v come in", addr)
	r := inc(ip)
	defer dec(ip)
	if r == 1 {
		io.WriteString(conn, "Exceed limit! You can create 3 connections at once!\n")
		log.Warningf("exceed limit, max connection count:%v", addr)
		return
	}
	if r == 2 {
		io.WriteString(conn, "Server overload\n")
		log.Warningf("server overload!%v", addr)
		return
	}

	buf := make([]byte, 4)
	var content string
	f, _ := conn.File()
	for {
		len, err := conn.Read(buf)
		syscall.SetsockoptInt(int(f.Fd()), syscall.IPPROTO_TCP, syscall.TCP_QUICKACK, 0)
		if err != nil {
			log.Errorf("recv error:%v", err)
			return
		}
		content += string(bytes.Trim(buf, "\r\n"))
		content = parseContent(cli, content) // 提取输入中的命令
		log.Infof("<- %v len:%v data:%v", addr, len, buf[0:len])

		if option.echo {
			n, err := conn.Write(bytes.ToUpper(buf[0:len]))
			log.Infof("-> %v len:%v data:%v", addr, n, string(buf[0:len]))
			if err != nil {
				log.Errorf("send to %v error:%v", addr, err)
				return
			}
		}

		total += len
		if total > 20 {
			log.Warningf("%v send total > 20, exit:%v", addr, total)
			_, err := io.WriteString(conn, fmt.Sprintf("\nYou have sent more than [%v] bytes!\n", total))
			if err != nil {
				log.Errorf("write string error:%v", err)
			}
			return
		}
	}
}

func getIP(address string) string {
	parts := strings.Split(address, ":")
	if len(parts) != 2 {
		return address
	}
	return parts[0]
}

func inc(ip string) int {
	mu.Lock()
	defer mu.Unlock()
	connection[ip] += 1
	connectionCount += 1

	if connection[ip] > 3 {
		return 1
	}

	if connectionCount > 20 {
		return 2
	}
	return 0
}

func dec(ip string) {
	mu.Lock()
	defer mu.Unlock()
	connection[ip] -= 1
	connectionCount -= 1
}

func parseContent(cli *Client, content string) string {
	conn := cli.conn
	option := cli.option
	if idx := strings.Index(content, "nodelay"); idx >= 0 {
		log.Infof("%v", "set nodelay")
		conn.SetNoDelay(true)
		option.delay = true
		return content[6:]
	}

	if idx := strings.Index(content, "delay"); idx >= 0 {
		log.Infof("%v", "set delay")
		conn.SetNoDelay(false)
		option.delay = false
		return content[5:]
	}
	if idx := strings.Index(content, "noecho"); idx >= 0 {
		log.Infof("%v", "set noecho")
		option.echo = false
		return content[6:]
	}
	if idx := strings.Index(content, "echo"); idx >= 0 {
		log.Infof("%v", "set echo")
		option.echo = true
		return content[4:]
	}
	return content
}
