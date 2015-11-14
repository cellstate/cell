package services

import (
	"log"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/net/ipv4"
)

type Discovery interface {
	Start() error
	Stop() error
	FindAny(cancel chan os.Signal) error
}

func NewSerfDiscovery(serf Serf, iface *net.Interface, group net.IP, self net.IP) (Discovery, error) {
	return &serfDiscovery{
		iface: iface,
		serf:  serf,
		self:  self,
		group: group,
		stop:  make(chan struct{}),
	}, nil
}

type serfDiscovery struct {
	iface *net.Interface
	group net.IP
	self  net.IP
	pconn *ipv4.PacketConn
	serf  Serf
	stop  chan struct{}
}

func (s *serfDiscovery) FindAny(cancel chan os.Signal) error {
	for {
		members, err := s.serf.Members()
		if err != nil {
			log.Printf("Failed to rerieve members list, retrying...")
			select {
			case <-cancel:
				return ErrUserCancelled
			case <-time.After(time.Second * 2):
				continue
			}
		}

		if len(members) > 1 {
			break
		}

		log.Printf("Found only %d alive gossip member (itself), multicasting to find others...", len(members))
		s.pconn.SetTOS(0x0)
		s.pconn.SetTTL(16)
		dst := &net.UDPAddr{IP: s.group, Port: 1024}
		if err := s.pconn.SetMulticastInterface(s.iface); err != nil {
			return err
		}

		s.pconn.SetMulticastTTL(2)
		if _, err := s.pconn.WriteTo([]byte{}, nil, dst); err != nil {
			return err
		}

		select {
		case <-cancel:
			return ErrUserCancelled
		case <-time.After(time.Second * 10):
		}
	}

	return nil
}

func (s *serfDiscovery) Start() error {
	conn, err := net.ListenPacket("udp4", "0.0.0.0:1024")
	if err != nil {
		return err
	}

	s.pconn = ipv4.NewPacketConn(conn)
	if err := s.pconn.JoinGroup(s.iface, &net.UDPAddr{IP: s.group}); err != nil {
		conn.Close()
		return err
	}

	if err := s.pconn.SetControlMessage(ipv4.FlagDst, true); err != nil {
		conn.Close()
		return err
	}

	go func() {
		<-s.stop
		conn.Close()
	}()

	go func() {
		b := make([]byte, 1500)
		for {
			_, cm, src, err := s.pconn.ReadFrom(b)
			if err != nil {
				if strings.Contains(err.Error(), "closed network connection") {
					log.Printf("Closed connection, stopping discovery listener...")
					return
				}

				log.Printf("Failed to read packet: %s", err)
				continue
			}

			if cm.Dst.IsMulticast() {
				if cm.Dst.Equal(s.group) {
					sip, _, err := net.SplitHostPort(src.String())
					if err != nil {
						log.Printf("Multicast src '%s' has unexpected format: %s", src, err)
					}

					if sip == s.self.String() {
						continue
					}

					err = s.serf.Join(sip)
					if err != nil {
						log.Printf("Failed to join serf gossip at '%s': %s ", sip, err)
					}
				} else {
					continue
				}
			}
		}
	}()

	return nil
}

func (s *serfDiscovery) Stop() error {
	s.stop <- struct{}{}
	return nil
}
