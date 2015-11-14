package main

import (
	"log"
	"net"
	"os"
	"os/signal"

	"github.com/codegangsta/cli"

	"github.com/cellstate/cell/clients/zerotier"
	"github.com/cellstate/cell/services"
)

// var ErrUserCancelled = errors.New("User cancelled")

//we start listening for join multicast messages over udp
// func listenForGossipJoins(serf services.Serf, iface *net.Interface, group net.IP, ip net.IP) (*ipv4.PacketConn, error) {
// 	conn, err := net.ListenPacket("udp4", "0.0.0.0:1024")
// 	if err != nil {
// 		return nil, err
// 	}

// 	pconn := ipv4.NewPacketConn(conn)
// 	if err := pconn.JoinGroup(iface, &net.UDPAddr{IP: group}); err != nil {
// 		conn.Close()
// 		return nil, err
// 	}

// 	if err := pconn.SetControlMessage(ipv4.FlagDst, true); err != nil {
// 		conn.Close()
// 		return nil, err
// 	}

// 	go func() {
// 		defer conn.Close()

// 		b := make([]byte, 1500)
// 		for {
// 			_, cm, src, err := pconn.ReadFrom(b)
// 			if err != nil {
// 				log.Printf("Failed to read packet: %s", err)
// 				continue
// 			}

// 			if cm.Dst.IsMulticast() {
// 				if cm.Dst.Equal(group) {
// 					sip, _, err := net.SplitHostPort(src.String())
// 					if err != nil {
// 						log.Printf("Multicast src '%s' has unexpected format: %s", src, err)
// 					}

// 					if sip == ip.String() {
// 						continue
// 					}

// 					err = serf.Join(sip)
// 					if err != nil {
// 						log.Printf("Failed to join serf gossip at '%s': %s ", sip, err)
// 					}
// 				} else {
// 					continue
// 				}
// 			}
// 		}
// 	}()

// 	return pconn, nil
// }

//multicast a tcp endpoint that hopefully receives that asks
//other members of the network to come play, it will stop once
//it knows of at least one member other than himself
// func multicastGossipJoinRequest(exit chan os.Signal, serf services.Serf, iface *net.Interface, group net.IP, ip net.IP, pconn *ipv4.PacketConn) error {

// 	for {
// 		members, err := serf.Members()
// 		if err != nil {
// 			log.Printf("Failed to rerieve members list, retrying...")
// 			select {
// 			case <-exit:
// 				return ErrUserCancelled
// 			case <-time.After(time.Second * 2):
// 				continue
// 			}
// 		}

// 		if len(members) > 1 {
// 			break
// 		}

// 		log.Printf("Found only %d alive gossip member (itself), multicasting to find others...", len(members))
// 		pconn.SetTOS(0x0)
// 		pconn.SetTTL(16)
// 		dst := &net.UDPAddr{IP: group, Port: 1024}
// 		if err := pconn.SetMulticastInterface(iface); err != nil {
// 			return err
// 		}

// 		pconn.SetMulticastTTL(2)
// 		if _, err := pconn.WriteTo([]byte{}, nil, dst); err != nil {
// 			return err
// 		}

// 		select {
// 		case <-exit:
// 			return ErrUserCancelled
// 		case <-time.After(time.Second * 10):
// 		}
// 	}

// 	return nil
// }

func joinAction(c *cli.Context) {
	network := c.Args().First()
	if network == "" {
		log.Fatalf("Failed, Please provide the network ID to join as the first argument")
	}

	exit := make(chan os.Signal)
	signal.Notify(exit, os.Interrupt, os.Kill)
	defer log.Println("Exited!")

	var err error
	var zeroc *zerotier.Client
	token := c.GlobalString("token")
	if token != "" {
		zeroc, err = zerotier.NewClient(token)
		if err != nil {
			log.Fatalf("Failed to create zerotier http client with token '%s'", token)
		}
	}

	//
	// ZeroTier service
	//

	ztier, err := services.NewZeroTier()
	if err != nil {
		log.Fatalf("Failed to create zerotier service: %s", err)
	}

	log.Printf("Starting zerotier and waiting for identity...")
	member, err := ztier.Start()
	if err != nil {
		log.Fatalf("Failed to join network: %s", err)
	}

	defer func() {
		log.Printf("Stopping ZeroTier service...")
		err := ztier.Stop()
		if err != nil {
			log.Fatalf("Failed to stop zerotier: %s", err)
		}
	}()

	if zeroc != nil {
		log.Printf("We have a zerotier api client, authorizing ourself (member '%s')...", member)
		err := zeroc.AuthorizeMember(network, member)
		if err != nil {
			log.Printf("Warning: Failed to authorize itself: '%s'. You might need to authorize member '%s' manually", err, member)
		}
	}

	log.Printf("Joining network '%s' and waiting for ip address...", network)
	ip, iface, err := ztier.Join(network, c.String("interface"), exit)
	if err != nil {
		if err == services.ErrUserCancelled {
			return
		}

		log.Fatalf("Failed to join network: %s", err)
	}

	//
	// Gossip service
	//
	sconf := services.SerfConf{
		Bind: ip.String(),
	}

	serf, err := services.NewSerf(sconf)
	if err != nil {
		log.Fatalf("Failed to create serf service: %s", err)
	}

	log.Printf("Joined network as member '%s', unicast available on '%s', starting serf...", member, ip.String())
	err = serf.Start()
	if err != nil {
		log.Fatalf("Failed to start serf: %s", err)
	}

	defer func() {
		log.Printf("Stopping serf service...")
		err := serf.Stop()
		if err != nil {
			log.Fatalf("Failed to stop serf: %s", err)
		}
	}()

	//
	// Discovery service
	//

	discovery, err := services.NewSerfDiscovery(serf, iface, net.ParseIP(c.String("group")), ip)
	if err != nil {
		log.Fatalf("Failed to create discovery service: %s", err)
	}

	log.Printf("Start listening for serf discovery (multicast group '%s') on interface '%s'...", c.String("group"), iface.Name)
	err = discovery.Start()
	if err != nil {
		log.Fatalf("Failed to start listening: %s", err)
	}

	defer func() {
		log.Printf("Stopping discovery service...")
		err := discovery.Stop()
		if err != nil {
			log.Fatalf("Failed to stop discovery: %s", err)
		}
	}()

	log.Printf("Searching for any gossip to join...")
	err = discovery.FindAny(exit)
	if err != nil {
		if err == services.ErrUserCancelled {
			return
		}

		log.Fatalf("Failed to start multicasting: %s", err)
	}

	log.Printf("Gossip is up and running!")
	<-exit //block until signal
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
