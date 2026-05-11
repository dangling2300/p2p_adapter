package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pion/stun"
	"github.com/urfave/cli"
)

// VERSION is injected by buildflags
var VERSION = "SELFBUILD"

func stun_decode(config *Config, recv []byte) {
	// Decoding XOR-MAPPED-ADDRESS attribute from message.
	if !stun.IsMessage(recv) {
		return
	}
	var xorAddr stun.XORMappedAddress
	m := new(stun.Message)
	m.Raw = recv
	decErr := m.Decode()
	if decErr != nil {
		log.Println("STUN decode:", decErr)
		return
	}
	if err := xorAddr.GetFrom(m); err != nil {
		log.Println("STUN ", err)
		return
	}
	if xorAddr.IP.To4() == nil {
		config.DefaultAddrSelf = fmt.Sprintf("[%v]:%v", xorAddr.IP.String(), xorAddr.Port)
		log.Println("STUN learned my ip:", config.DefaultAddrSelf)
	} else {
		config.DefaultAddrSelf = fmt.Sprintf("%v:%v", xorAddr.IP.String(), xorAddr.Port)
		log.Println("STUN learned my ip:", config.DefaultAddrSelf)
	}

}
func stun_send(stun_addr *net.UDPAddr, remote_conn *net.UDPConn) {

	message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	_, err := remote_conn.WriteToUDP(message.Raw, stun_addr)
	if err != nil {
		log.Printf("fail to send STUN %v", err)
	}
}

func get_peer_addr(config *Config) *net.UDPAddr {
	queryURL := config.HTTPLink + "/get?peer=" + url.QueryEscape(config.NamePeer) + "&setpeer=" + url.QueryEscape(config.NameSelf) + "&setaddr=" + url.QueryEscape(config.DefaultAddrSelf)
	body, err := http_get_string(config, queryURL)
	//log.Println("http_get_string", body, queryURL)
	if err != nil || body == "" {
		return nil
	}
	return resolveAddrAccordingToPublicListen(config, string(body))
}

func http_get_string(config *Config, http_get_link string) (string, error) {

	// if config.HTTPProxy != "" then set proxy_func to http.ProxyURL(proxyUrl) else set proxy_func to nil
	//set timeout to 15s
	//log.Println(http_link) url.QueryEscape(retaddr)
	proxy_url := config.HTTPProxy

	var proxy_func func(*http.Request) (*url.URL, error)
	if proxy_url != "" {
		proxyUrl, err := url.Parse(proxy_url)
		if err != nil {
			log.Println(err)
			return "", err
		}
		proxy_func = http.ProxyURL(proxyUrl)
	} else {
		proxy_func = nil
	}

	transport := &http.Transport{
		Proxy:                 proxy_func,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	}

	client := &http.Client{Transport: transport}
	defer client.CloseIdleConnections()

	resp, err := client.Get(http_get_link)
	if err != nil {
		log.Println(err)
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println(err)
		return "", err
	}
	return string(body), nil
}

func resolveAddrAccordingToPublicListen(config *Config, body string) *net.UDPAddr {
	var ret *net.UDPAddr

	host, _, err := net.SplitHostPort(config.PublicListen)
	if err == nil {

		if net.ParseIP(host).To4() == nil {
			ret, err = net.ResolveUDPAddr("udp6", body)

			if err != nil {
				log.Println("resolving PublicListen ipv6 error", err)
				return nil
			}
		} else {
			ret, err = net.ResolveUDPAddr("udp4", body)
			if err != nil {
				log.Println("resolving PublicListen ipv4 error", err)
				return nil
			}
		}

	} else {
		//fmt.Println("Invalid PublicListen:", err)

		ret, err = net.ResolveUDPAddr("udp", body)
		if err != nil {
			log.Println("resolving PublicListen error", err)

			return nil
		}

	}

	return ret
}
func fatalError(err error) {
	if err != nil {
		log.Fatalf("%+v\n", err)
		os.Exit(-1)
	}
}
func main() {
	if VERSION == "SELFBUILD" {
		// add more log flags for debugging
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	myApp := cli.NewApp()
	myApp.Name = "server"
	myApp.Usage = "server"
	myApp.Version = VERSION
	myApp.Flags = []cli.Flag{

		cli.StringFlag{
			Name:  "log",
			Value: "",
			Usage: "specify a log file to output, default goes to stderr",
		},
		cli.BoolFlag{
			Name:  "quiet",
			Usage: "to suppress the 'stream open/close' messages",
		},

		cli.StringFlag{
			Name:  "c",
			Value: "server.json", // when the value is not empty, the config path must exists
			Usage: "config from json file, which will override the command from shell",
		},
	}

	myApp.Action = func(c *cli.Context) error {
		configs := Configs{}

		log.Println("version:", VERSION)
		if c.String("c") != "" {
			err := parseJSONConfig(&configs, c.String("c"))
			fatalError(err)
		}
		for i, config := range configs {

			newFunction(config, c, i)
		}
		select {}

	}
	myApp.Run(os.Args)
}
func newFunction(config Config, c *cli.Context, config_id int) {
	config.Log = c.String("log")

	// log redirect
	if config.Log != "" {
		f, err := os.OpenFile(config.Log, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		fatalError(err)
		defer f.Close()
		log.SetOutput(f)
	}
	if config.AutoExpire == 0 {
		config.AutoExpire = 10
	}

	if config.NameSelf != "" && config.NamePeer != "" {
		config.DefaultAddrSelf = resolveAddrAccordingToPublicListen(&config, config.DefaultAddrSelf).String()
		log.Println(config_id, "DefaultAddrSelf: ", config.DefaultAddrSelf)
		var private_conn *net.UDPConn = nil
		var private_sendto *net.UDPAddr = &net.UDPAddr{}
		var public_conn *net.UDPConn = nil
		var public_sendto *net.UDPAddr = &net.UDPAddr{}
		var last_recv_time time.Time = time.Now().Add(-time.Duration(config.AutoExpire) * time.Second)
		var stun_addr_list []*net.UDPAddr = nil
		if config.StunAddr != "" {
			stun_addr, err := net.ResolveUDPAddr("udp", config.StunAddr)
			stun_addr_list = append(stun_addr_list, stun_addr)
			if err != nil {
				log.Println(config_id, "STUN error ", err)
			}
		}
		if config.StunListURL != "" {
			// use proxies to get the list of STUN servers
			stunlist, err := http_get_string(&config, config.StunListURL)
			if err != nil {
				log.Println(config_id, "STUN list error", err)
			} else {
				// split the list by newline
				stunServers := strings.Split(stunlist, "\n")
				for _, server := range stunServers {
					server = strings.TrimSpace(server)
					if server == "" {
						continue
					}
					stunAddr, err := net.ResolveUDPAddr("udp", server)
					if err != nil {
						log.Println(config_id, "STUN resolve error:", err)
						continue
					}
					stun_addr_list = append(stun_addr_list, stunAddr)
				}
			}

		}
		stunAddrMap := make(map[uint64]*net.UDPAddr)
		hash := func(addr *net.UDPAddr) uint64 {
			ip := addr.IP.To16()
			port := uint64(addr.Port)
			var hash uint64
			for i := 0; i < len(ip); i++ {
				hash = hash*31 + uint64(ip[i])
			}
			return hash*31 + port
		}
		for _, addr := range stun_addr_list {
			stunAddrMap[hash(addr)] = addr
			log.Println(config_id, "STUN server:", addr)
		}

		if _, _, err := net.SplitHostPort(config.PrivateListen); err == nil {
			addr, err := net.ResolveUDPAddr("udp", config.PrivateListen)
			fatalError(err)
			private_conn, err = net.ListenUDP("udp", addr)
			fatalError(err)

		}
		if _, _, err := net.SplitHostPort(config.PrivateSendto); err == nil {
			var err error
			private_sendto, err = net.ResolveUDPAddr("udp", config.PrivateSendto)
			fatalError(err)
			if private_conn == nil {
				private_conn, err = net.ListenUDP("udp", nil)
				fatalError(err)
			}

		}
		log.Println(config_id, "private listen on:", private_conn.LocalAddr())
		log.Println(config_id, "private default sendto:", private_sendto)
		if config.PublicListen == "" {
			var err error
			public_conn, err = net.ListenUDP("udp", nil)
			fatalError(err)
		} else {
			remote_addr, err := net.ResolveUDPAddr("udp", config.PublicListen)
			fatalError(err)
			public_conn, err = net.ListenUDP("udp", remote_addr)
			fatalError(err)
		}

		log.Println(config_id, "public listen on:", public_conn.LocalAddr())

		go func() {
			for {
				if time.Since(last_recv_time) > time.Duration(config.AutoExpire)*time.Second {
					if len(stun_addr_list) > 0 {
						stun_addr := stun_addr_list[rand.Intn(len(stun_addr_list))]
						stun_send(stun_addr, public_conn)
						log.Println(config_id, "STUN sendto:", stun_addr)
						stun_addr = stun_addr_list[0]
						stun_send(stun_addr, public_conn)
						log.Println(config_id, "STUN sendto:", stun_addr)
					}
					ret := get_peer_addr(&config)
					if ret != nil {
						log.Println(config_id, "public sendto (from HTTP):", ret)
						public_sendto = ret
					} else {
						//log.Println("public sendto (from HTTP, not updated):", public_sendto)
					}
					// create a remote_conn and send a UDP message to the remote_addr
					public_conn.WriteTo([]byte("hello"), public_sendto)

				}
				time.Sleep(time.Duration(config.AutoExpire) * time.Second / 2)
			}
		}()

		relay := func(from_outer_to_inner bool) {
			buffer := make([]byte, 3000)
			for {
				src := private_conn
				dst := public_conn
				src_addr := private_sendto
				dst_addr := public_sendto
				if from_outer_to_inner {
					src = public_conn
					dst = private_conn
					src_addr = public_sendto
					dst_addr = private_sendto
				}
				if src != nil && dst != nil && dst_addr != nil {
					n, raddr, err := src.ReadFromUDP(buffer)
					if err != nil {
						log.Println(err)
						continue
					}
					//log.Println("read from:", hash(raddr), raddr)
					check_from_hash_map := stunAddrMap[hash(raddr)]
					if check_from_hash_map != nil && check_from_hash_map.String() == raddr.String() {
						log.Println(config_id, "STUN from:", raddr)
						stun_decode(&config, buffer[:n])
						continue
					}
					if src_addr.Port != raddr.Port || (!src_addr.IP.Equal(raddr.IP)) {
						*src_addr = *raddr
						if from_outer_to_inner {
							log.Println(config_id, "public sendto:", src_addr)
						} else {
							log.Println(config_id, "private sendto:", src_addr)
						}
					}

					_, err = dst.WriteToUDP(buffer[:n], dst_addr)
					if err != nil {
						log.Println(err)
						continue
					}

					if from_outer_to_inner {
						last_recv_time = time.Now()
						//log.Println("read from:", addr)
					} else {
						//log.Println("write to:", dst_addr)
					}
				} else {
					if src == nil {
						if from_outer_to_inner {
							log.Println(config_id, "public can not be read")
						} else {
							log.Println(config_id, "private can not be read")
						}

					}
					if dst == nil || dst_addr == nil {
						if from_outer_to_inner {
							log.Println(config_id, "private can not be sendto")
						} else {
							log.Println(config_id, "public can not be sendto")
						}
					}
					time.Sleep(100 * time.Millisecond)
				}
			}
		}
		go relay(true)
		go relay(false)
	}

	if config.HTTPListen != "" {
		remote_address_list := make(map[string]*net.UDPAddr)
		go func() {

			http.HandleFunc("/get", func(w http.ResponseWriter, r *http.Request) {
				r.ParseForm()
				peer := r.FormValue("peer")
				setpeer := r.FormValue("setpeer")
				setaddr := r.FormValue("setaddr")
				if peer == "" {
					// return the list of peers
					for k, v := range remote_address_list {
						fmt.Fprintln(w, k, v)
					}
				} else {

					if remote_address_list[peer] == nil {
						fmt.Fprint(w, "")
					} else {
						fmt.Fprint(w, remote_address_list[peer].String())
					}
				}
				if setaddr != "" && setpeer != "" {
					addr2, err := net.ResolveUDPAddr("udp", setaddr)
					if err != nil || addr2 == nil {
						log.Println(config_id, "remote address error", err)
						return
					}
					remote_address_list[setpeer] = addr2
				}
			})

			err := http.ListenAndServe(config.HTTPListen, nil)
			fatalError(err)
			log.Println(config_id, "HTTP Listen on: ", config.HTTPListen)
		}()
	}

}
