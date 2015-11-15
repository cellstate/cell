package commands

import (
	"log"
	"net"
	"os"
	"os/signal"

	"github.com/codegangsta/cli"

	"github.com/cellstate/cell/clients/zerotier"
	"github.com/cellstate/cell/services"
)

var Join = cli.Command{
	Name:  "join",
	Usage: "...",
	Flags: []cli.Flag{
		cli.StringFlag{Name: "interface,i", Value: "zt0", Usage: "..."},
		cli.StringFlag{Name: "group,g", Value: "224.0.0.250", Usage: "..."},
	},
	Action: func(c *cli.Context) {

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
		// Exchange Service
		//
		exchange, err := services.NewDeluge()
		if err != nil {
			log.Fatalf("Failed to create exchange service: %s", err)
		}

		log.Printf("Starting Deluge BitTorrent daemon...")
		err = exchange.Start()
		if err != nil {
			log.Fatalf("Failed to start exchange service: %s", err)
		}

		defer func() {
			log.Printf("Stopping Exchange service...")
			err := exchange.Stop()
			if err != nil {
				log.Fatalf("Failed to stop exchange: %s", err)
			}
		}()

		//
		// VPN service
		//

		vpn, err := services.NewZeroTier()
		if err != nil {
			log.Fatalf("Failed to create zerotier service: %s", err)
		}

		log.Printf("Starting zerotier and waiting for identity...")
		member, err := vpn.Start()
		if err != nil {
			log.Fatalf("Failed to join network: %s", err)
		}

		defer func() {
			log.Printf("Stopping ZeroTier service...")
			err := vpn.Stop()
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
		ip, iface, err := vpn.Join(network, c.String("interface"), exit)
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

		gossip, err := services.NewSerf(sconf)
		if err != nil {
			log.Fatalf("Failed to create gossip service: %s", err)
		}

		log.Printf("Joined network as member '%s', unicast available on '%s', starting gossip service...", member, ip.String())
		err = gossip.Start()
		if err != nil {
			log.Fatalf("Failed to start gossip: %s", err)
		}

		defer func() {
			log.Printf("Stopping gossip service...")
			err := gossip.Stop()
			if err != nil {
				log.Fatalf("Failed to stop gossip: %s", err)
			}
		}()

		//
		// Discovery service
		//

		discovery, err := services.NewSerfDiscovery(gossip, iface, net.ParseIP(c.String("group")), ip)
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

	},
}
