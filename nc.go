package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"golang.org/x/net/proxy"
)

var verbose bool

type Packet struct {
	size int
	data [1500]byte
}

func ConnToChan(src io.Reader, ch chan Packet, done chan int) {
	defer func() {
		done <- 0
	}()
	defer close(ch)
	var buffer Packet
	bytes, err := src.Read(buffer.data[:])
	if err != nil {
		if IsEOF(err) {
			buffer.size = bytes
		} else {
			log.Printf("Read error: %s\n", err.Error())
			buffer.size = -1
		}
	} else {
		buffer.size = bytes
	}
	ch <- buffer
	for buffer.size > 0 {
		bytes, err := src.Read(buffer.data[:])
		if err != nil {
			if IsEOF(err) {
				buffer.size = bytes
			} else {
				log.Printf("Read error: %s\n", err.Error())
				buffer.size = -1
			}
		} else {
			buffer.size = bytes
		}
		ch <- buffer
	}
}

func ChanToConn(ch chan Packet, tgt io.Writer, done chan int) {
	defer func() {
		done <- 0
	}()
	defer close(ch)
	var buffer Packet
	buffer, ok := <-ch
	for ok && buffer.size > 0 {
		_, err := tgt.Write(buffer.data[0:buffer.size])
		if err != nil {
			log.Printf("Write error: %s\n", err.Error())
			return
		}
		buffer, ok = <-ch
	}
}

func doConn(con net.Conn) {
	ch_stdout := make(chan Packet)
	ch_remote := make(chan Packet)
	complete := make(chan int)

	go ConnToChan(con, ch_stdout, complete)
	go ChanToConn(ch_stdout, os.Stdout, complete)

	go ConnToChan(os.Stdin, ch_remote, complete)
	go ChanToConn(ch_remote, con, complete)

	_, _ = <-complete
}

func IsEOF(err error) bool {
	if err == nil {
		return false
	} else if err == io.EOF {
		return true
	} else if oerr, ok := err.(*net.OpError); ok {
		if oerr.Err.Error() == "use of closed network connection" {
			return true
		}
	} else {
		if err.Error() == "use of closed network connection" {
			return true
		}
	}
	return false
}

func main() {
	var listen string
	var destinationPort string
	var isListen bool
	var host string
	var proxyhost string
	var proxyprotocol string
	var proxyuser string
	var proxypw string

	flag.StringVar(&proxyhost, "x", "", "proxy host in the form proxy-server:port or http...., depends on protocol")
	flag.StringVar(&proxyprotocol, "X", "", "proxy protocol: 5 (socks) or connect")
	flag.StringVar(&proxyuser, "u", "", "proxy userid")
	flag.StringVar(&proxypw, "p", "", "proxy password")
	flag.StringVar(&listen, "l", "", "listen to port number n, :n or b:n, where n is port number, b is binding inteface, defaults to 0")
	flag.BoolVar(&verbose, "v", false, "Noisy")
	flag.Parse()
	if flag.NFlag() == 0 && flag.NArg() == 0 {
		fmt.Println("\nSimplified nc [-v] [-l port] or [hostname port]\n")
		flag.Usage()
		fmt.Println(`
Examples:
	nc -l 9899

	means listen on port 9899

	nc localhost 6767

	means connect to port 6767`)

		os.Exit(1)
	}
	if listen != "" {
		isListen = true
		ret := strings.SplitN(listen, ":", 2)
		port := ret[len(ret)-1]
		if v, err := strconv.Atoi(port); err != nil || (v > 65535 || v < 1) {
			log.Println("Listen port must be an integer between from 1-65536")
			os.Exit(1)
		}
		if len(ret) == 1 {
			listen = ":" + port
		} else if ret[0] == "" {
			listen = ":" + port
		}

		if flag.NArg() != 0 {
			log.Printf("Listen mode on: Arguments %v are extra\n", flag.Args())
			os.Exit(1)
		}
		if verbose {
			log.Println("Listen: ", listen)
		}
	} else {
		if flag.NArg() != 2 {
			log.Println("Listen mode off: [hostname] [port] are mandatory arguments")
			os.Exit(1)
		}
		if v, err := strconv.Atoi(flag.Arg(1)); err != nil || (v > 65535 || v < 1) {
			log.Println("Destination port must be an integer between from 1-65536")
			os.Exit(1)
		}
		host = flag.Arg(0)
		destinationPort = ":" + flag.Arg(1)
		if verbose {
			log.Println("Hostname:", host)
			log.Println("Port:", destinationPort)
		}
	}

	if isListen {
		listener, err := net.Listen("tcp", listen)
		if err != nil {
			log.Fatalln(err)
		}
		if verbose {
			log.Println("Listening on", listen)
		}
		con, err := listener.Accept()
		if err != nil {
			log.Fatalln(err)
		}
		if verbose {
			log.Println("Connect from", con.RemoteAddr())
		}
		doConn(con)

	} else if host != "" {
		switch proxyprotocol {

		case "5":
			if proxyhost != "" {
				if verbose {
					log.Println("use socks5 protocol to connect to ", proxyhost)
				}
				auth := proxy.Auth{
					User:     proxyuser,
					Password: proxypw,
				}
				dialer, err := proxy.SOCKS5("tcp", proxyhost, &auth, proxy.Direct)
				if err != nil {
					log.Fatalln(err)
				}
				con, err := dialer.Dial("tcp", host+destinationPort)
				if err != nil {
					log.Fatalln(err)
				}
				if verbose {
					log.Println("Connected to", host+destinationPort)
				}
				doConn(con)
			} else {
				log.Fatalln("Proxy protocal specified, but not proxy server specified.")
			}
		case "connect":
			if proxyhost != "" {
				if verbose {
					log.Println("use http protocol to connect to ", proxyhost)
				}
				proxyUrl, err := url.Parse(proxyhost)
				if err != nil {
					log.Fatalln(err)
				}
				dialer, err := proxy.FromURL(proxyUrl, proxy.Direct)
				if err != nil {
					log.Fatalln(err)
				}
				con, err := dialer.Dial("tcp", host+destinationPort)
				if err != nil {
					log.Fatalln(err)
				}
				if verbose {
					log.Println("Connected to", host+destinationPort)
				}
				doConn(con)
			} else {
				log.Fatalln("Proxy protocal specified, but not proxy server specified.")
			}
		case "":
			con, err := net.Dial("tcp", host+destinationPort)
			if err != nil {
				log.Fatalln(err)
			}
			if verbose {
				log.Println("Connected to", host+destinationPort)
			}
			doConn(con)
		default:
			log.Fatalln("Unknown protocolcol")
		}
	} else {
		flag.Usage()
	}
}
