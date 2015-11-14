package services

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"time"
)

func NewZeroTier() (ZeroTier, error) {
	return &zeroProcess{}, nil
}

type ZeroTier interface {
	Start() (memberID string, err error)
	Join(network, iface string, cancel chan os.Signal) (net.IP, *net.Interface, error)
	Stop() error
}

//runs zerotier as seperate process
type zeroProcess struct {
	*os.Process
}

func (z *zeroProcess) Start() (string, error) {
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

	z.Process = cmd.Process
	return member, nil
}

//@todo can we get the iface name from the network join?
func (z *zeroProcess) Join(network, iface string, cancel chan os.Signal) (net.IP, *net.Interface, error) {
	cmd := exec.Command("zerotier-cli", "join", network)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Start()
	if err != nil {
		return nil, nil, err
	}

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
		case <-cancel:
			return nil, nil, ErrUserCancelled
		case <-time.After(time.Second):
		}

	}

	return nil, nil, nil
}

func (z *zeroProcess) Stop() error {
	z.Process.Signal(os.Interrupt)

	_, err := z.Process.Wait()
	if err != nil {
		return err
	}

	return nil
}
