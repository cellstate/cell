package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

func initSerf(ip net.IP) error {
	cmd := exec.Command("serf", "agent", fmt.Sprintf("-bind=%s", ip))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}

//we start listening for join multicast messages over udp
func listenForGossipJoins(exit chan os.Signal, iface *net.Interface, group net.IP, ip net.IP) (*ipv4.PacketConn, error) {
	conn, err := net.ListenPacket("udp4", "0.0.0.0:1024")
	if err != nil {
		return nil, err
	}

	pconn := ipv4.NewPacketConn(conn)
	if err := pconn.JoinGroup(iface, &net.UDPAddr{IP: group}); err != nil {
		conn.Close()
		return nil, err
	}

	if err := pconn.SetControlMessage(ipv4.FlagDst, true); err != nil {
		conn.Close()
		return nil, err
	}

	go func() {
		defer conn.Close()

		b := make([]byte, 1500)
		for {
			_, cm, src, err := pconn.ReadFrom(b)
			if err != nil {
				log.Printf("Failed to read packet: %s", err)
				continue
			}

			if cm.Dst.IsMulticast() {
				if cm.Dst.Equal(group) {
					sip, _, err := net.SplitHostPort(src.String())
					if err != nil {
						log.Printf("Multicast src '%s' has unexpected format: %s", src, err)
					}

					if sip == ip.String() {
						continue
					}

					cmd := exec.Command("serf", "join", sip)
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
					err = cmd.Run()
					if err != nil {
						log.Printf("Failed to join serf gossip at '%s': %s ", sip, err)
					}
				} else {
					continue
				}
			}
		}
	}()

	return pconn, nil
}

//multicast a tcp endpoint that hopefully receives that asks
//other members of the network to come play, it will stop once
//it knows of at least one member other than himself
func multicastGossipJoinRequest(exit chan os.Signal, iface *net.Interface, group net.IP, ip net.IP, pconn *ipv4.PacketConn) error {

	for {
		r, w := io.Pipe()
		cmd := exec.Command("serf", "members", "-status=alive", "-format=json")
		cmd.Stderr = os.Stderr
		cmd.Stdout = w
		err := cmd.Start()
		if err != nil {
			return err
		}

		v := struct {
			Members []struct {
				Name string `json:"name`
			} `json:"members"`
		}{}

		dec := json.NewDecoder(r)
		err = dec.Decode(&v)
		if err != nil {
			log.Printf("Failed to contact serf daemon, retrying...")
			select {
			case <-exit:
				return ErrUserCancelled
			case <-time.After(time.Second * 2):
				continue
			}
		}

		if len(v.Members) > 1 {
			break
		}

		log.Printf("Found only %d alive gossip member (itself), multicasting to find others...", len(v.Members))
		pconn.SetTOS(0x0)
		pconn.SetTTL(16)
		dst := &net.UDPAddr{IP: group, Port: 1024}
		if err := pconn.SetMulticastInterface(iface); err != nil {
			return err
		}

		pconn.SetMulticastTTL(2)
		if _, err := pconn.WriteTo([]byte{}, nil, dst); err != nil {
			return err
		}

		select {
		case <-exit:
			return ErrUserCancelled
		case <-time.After(time.Second * 10):
		}
	}

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

	log.Printf("Joined network as member '%s', unicast available on '%s', starting serf...", member, ip.String())
	err = initSerf(ip)
	if err != nil {
		log.Fatalf("Failed to start serf: %s", err)
	}

	group := net.ParseIP(c.String("group"))
	log.Printf("Start listening for UDP gossip joins (multicast group '%s') on interface '%s'...", group, iface.Name)
	pconn, err := listenForGossipJoins(exit, iface, group, ip)
	if err != nil {
		log.Fatalf("Failed to start listening: %s", err)
	}

	log.Printf("Start multicasting request to join gossip...")
	err = multicastGossipJoinRequest(exit, iface, group, ip, pconn)
	if err != nil {
		log.Fatalf("Failed to start multicasting: %s", err)
	}

	log.Printf("Gossip is up and running!")
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
				cli.StringFlag{Name: "group,g", Value: "224.0.0.250", Usage: "..."},
			},
		},
	}

	app.Run(os.Args)
}
