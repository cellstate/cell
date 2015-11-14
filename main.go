package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	"github.com/codegangsta/cli"
	"golang.org/x/net/ipv4"
)

var ErrUserCancelled = errors.New("User cancelled")

// start the vpn client and
// estabilish an connection
func initVPN(exit chan os.Signal, network string) (string, error) {

	//start zerotier service
	cmd := exec.Command("/var/lib/zerotier-one/zerotier-one")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	if err != nil {
		return "", err
	}

	member := ""
	for {
		data, err := ioutil.ReadFile("/var/lib/zerotier-one/identity.public")
		if os.IsNotExist(err) {
			select {
			case <-exit:
				return member, ErrUserCancelled
			case <-time.After(time.Second):
			}
		} else if err != nil {
			return member, err
		} else {
			parts := bytes.SplitN(data, []byte(":"), 2)
			if len(parts) < 2 {
				return member, fmt.Errorf("Unexpected identity file content: %s", data)
			}

			member = string(parts[0])
			break
		}
	}

	//start and join zerotier network
	cmd = exec.Command("zerotier-cli", "join", network)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Start()
	if err != nil {
		return member, err
	}

	return member, nil
}

//with a zerotier access token it is possible
//to authorize this member automatically through the api
func authMember(token, network, member string) error {

	loc := fmt.Sprintf("https://my.zerotier.com/api/network/%s/member/%s", network, member)
	req, err := http.NewRequest("POST", loc, strings.NewReader(fmt.Sprintf(`{"config":{"authorized": true}, "annot": {"description": "joined %s"}}`, time.Now().Format("02-01-2006 (15:04)"))))
	if err != nil {
		return fmt.Errorf("Failed to create request: %s", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	if resp.StatusCode > 299 {
		return fmt.Errorf("Failed to update member details '%v': %s", req.Header, resp.Status)
	}

	return nil
}

// wait for a network address beinn assigned to
// the vpn iterface
func initNetworking(exit chan os.Signal, iface string) (net.IP, *net.Interface, error) {
	for {
		ifaces, err := net.Interfaces()
		if err != nil {
			return nil, nil, err
		}

		for _, i := range ifaces {
			if i.Name == iface {
				addrs, err := i.Addrs()
				if err != nil {
					return nil, nil, err
				}

				if len(addrs) > 0 {
					ip, _, err := net.ParseCIDR(addrs[0].String())
					if err != nil {
						return nil, nil, err
					}

					if ip.To4() != nil {
						return ip, &i, nil
					}
				}
			}
		}

		select {
		case <-exit:
			return nil, nil, ErrUserCancelled
		case <-time.After(time.Second):
		}

	}

	return nil, nil, nil
}

//we start listening for join multicast messages over udp
func listenForGossipJoins(exit chan os.Signal, iface *net.Interface) error {
	group := net.IPv4(224, 0, 0, 250)
	conn, err := net.ListenPacket("udp4", "0.0.0.0:1024")
	if err != nil {
		return err
	}

	pconn := ipv4.NewPacketConn(conn)
	if err := pconn.JoinGroup(iface, &net.UDPAddr{IP: group}); err != nil {
		return err
	}

	if err := pconn.SetControlMessage(ipv4.FlagDst, true); err != nil {
		return err
	}

	go func() {
		defer conn.Close()
		b := make([]byte, 1500)
		for {
			n, cm, src, err := pconn.ReadFrom(b)
			log.Printf("read n: %d, cm: %s, src: %v, err: %s", n, cm, src, err)
			if cm.Dst.IsMulticast() {
				if cm.Dst.Equal(group) {
					log.Println("Joined group")
				} else {
					log.Println("Unkown group")
					continue
				}
			}
		}
	}()

	for {
		select {
		case <-exit:
			return ErrUserCancelled
		case <-time.After(time.Second):
		}

		pconn.SetTOS(0x0)
		pconn.SetTTL(16)
		dst := &net.UDPAddr{IP: group, Port: 1024}
		if err := pconn.SetMulticastInterface(iface); err != nil {
			return err
		}

		pconn.SetMulticastTTL(2)
		if _, err := pconn.WriteTo([]byte("abc"), nil, dst); err != nil {
			return err
		}

	}

	return nil
}

//we start broadcasting for a gossip to join
func broadcastGossipJoinRequest() error {

	return nil
}

func joinAction(c *cli.Context) {
	network := c.Args().First()
	if network == "" {
		log.Fatalf("Failed, Please provide the network ID to join as the first argument")
	}

	exit := make(chan os.Signal)
	signal.Notify(exit, os.Interrupt, os.Kill)

	log.Printf("Joining network '%s' and waiting for identity...", network)
	member, err := initVPN(exit, network)
	if err != nil {
		log.Fatalf("Failed to join network: %s", err)
	}

	if c.GlobalString("token") != "" {
		log.Printf("Saw zerotier token '%s', authorizing itself...", c.GlobalString("token"))
		err = authMember(c.GlobalString("token"), network, member)
		if err != nil {
			log.Printf("Warning: Failed to authorize itself: '%s'. You might need to authorize member '%s' manually", err, member)
		}
	}

	log.Printf("Waiting for network authorization and/or ip address...")
	ip, iface, err := initNetworking(exit, c.String("interface"))
	if err != nil {
		log.Fatalf("Failed to join network: %s", err)
	}

	log.Printf("Joined network as member '%s' reachable on ip '%s'", member, ip.String())
	log.Printf("Start listening for UDP gossip joins on interface '%s'...", iface.Name)
	err = listenForGossipJoins(exit, iface)
	if err != nil {
		log.Fatalf("Failed to start listening: %s", err)
	}

	log.Printf("Start broadcasting request to join gossip...")
	err = broadcastGossipJoinRequest()
	if err != nil {
		log.Fatalf("Failed to start broadcasting: %s", err)
	}

	<-exit
}

func main() {
	app := cli.NewApp()
	app.Name = "boom"
	app.Usage = "make an explosive entrance"
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "token,t", Usage: "..."},
	}

	app.Commands = []cli.Command{
		{
			Name:   "join",
			Usage:  "...",
			Action: joinAction,
			Flags: []cli.Flag{
				cli.StringFlag{Name: "interface,i", Value: "zt0", Usage: "..."},
			},
		},
	}

	app.Run(os.Args)
}
