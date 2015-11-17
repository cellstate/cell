package services

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/anacrolix/torrent/bencode"
)

func NewDeluge(gossip Gossip, ip net.IP) (Exchange, error) {
	return &delugeProcess{
		torrentPath: "/tmp",
		trackerBind: ":9000",
		filesBind:   ":8080",
		gossip:      gossip,
		ip:          ip,
	}, nil
}

type peer struct {
	PeerID string `bencode:"peer id"`
	IP     string `bencode:"ip"`
	Port   string `bencode:"port"`
}

type Exchange interface {
	Start() error
	Stop() error

	Benchmark() (string, error)
	CreateLink(name, path string) (string, error)
	SeedLink(turl, path string) error

	Pull(id string) error
}

type delugeProcess struct {
	torrentPath string
	trackerBind string
	filesBind   string
	gossip      Gossip
	ip          net.IP
	*os.Process
}

func (d *delugeProcess) Start() error {
	cmd := exec.Command("deluged", "-d", "-i", d.ip.String())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	if err != nil {
		return err
	}

	d.Process = cmd.Process

	//starts a super minimal bittorrent tracker that only returns peers
	go func() {
		torrents := map[string]map[string]peer{}
		log.Printf("Starting tracker on '%s'...", d.trackerBind)
		err = http.ListenAndServe(d.trackerBind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			//map[ipv6:[fe80::226:bbff:fe0a:e12b] info_hash:[?0FA??c??N???] left:[100000000] key:[B22982B1] event:[started] numwant:[200] compact:[1] no_peer_id:[1] peer_id:[-UM1870-{??qCzVB<ur] port:[40959] uploaded:[0] downloaded:[0] corrupt:[0]]
			err := r.ParseForm()
			if err != nil {
				log.Printf("Failed to parse announce form data: %s", err)
				return
			}

			host, port, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				log.Printf("Failed to parse remote host and port: %s", err)
				return
			}

			if r.Form.Get("peer_id") == "" {
				//@todo sometimes peer_id is empty?
				return
			}

			//add announcing peer to peerlist of torrent
			log.Printf("Torrent Client (peer_id: '%s', compact: %s) announces from %s:%s, port set to %s for info_hash '%x'", r.Form.Get("peer_id"), r.Form.Get("compact"), host, port, r.Form.Get("port"), r.Form.Get("info_hash"))
			peerlist, ok := torrents[r.Form.Get("info_hash")]
			if !ok {
				peerlist = map[string]peer{}
				torrents[r.Form.Get("info_hash")] = peerlist
			}

			peerlist[r.Form.Get("peer_id")] = peer{
				PeerID: r.Form.Get("peer_id"),
				IP:     host,
				Port:   r.Form.Get("port"),
			}

			peers := []peer{}
			for _, p := range peerlist {
				peers = append(peers, p)
			}

			if r.Form.Get("compact") == "1" {
				buff := bytes.NewBuffer(nil)
				err = d.writeCompactPeers(buff, peers)
				if err != nil {
					log.Printf("Error: %s", err)
					return
				}

				log.Printf("compact: '% x'", buff.Bytes())
				data := struct {
					Peers []byte `bencode:"peers"`
				}{
					Peers: buff.Bytes(),
				}

				//encode response
				log.Printf("Returning announce with: %+v", data)
				w.Header().Set("Content-Type", "text/plain")
				enc := bencode.NewEncoder(w)
				err = enc.Encode(data)
				if err != nil {
					log.Printf("Failed to bencode: %s", err)
					return
				}

			} else {
				data := struct {
					Peers []peer `bencode:"peers"`
				}{
					Peers: peers,
				}

				//encode response
				log.Printf("Returning announce with: %+v", data)
				w.Header().Set("Content-Type", "text/plain")
				enc := bencode.NewEncoder(w)
				err = enc.Encode(data)
				if err != nil {
					log.Printf("Failed to bencode: %s", err)
					return
				}
			}

		}))

		log.Printf("Tracker exited")
		if err != nil {
			log.Printf("Tracker failed: %s", err)
		}
	}()

	log.Printf("Torrent files can be downloaded at: '%s'", d.filesBind)
	go func() {
		log.Fatal(http.ListenAndServe(d.filesBind, http.FileServer(http.Dir(d.torrentPath))))
	}()

	return nil
}

func (d *delugeProcess) writeCompactPeers(b *bytes.Buffer, peer []peer) (err error) {
	for _, p := range peer {

		ip4 := net.ParseIP(p.IP).To4()
		log.Printf("ipv4: '% x' (%s)", ip4, ip4)
		_, err = b.Write(ip4)
		if err != nil {
			return err
		}

		port, err := strconv.Atoi(p.Port)
		if err != nil {
			return err
		}

		portBytes := []byte{byte(port >> 8), byte(port)}

		log.Printf("port: '% x' (%d)", portBytes, port)
		_, err = b.Write(portBytes)
		if err != nil {
			return err
		}
	}
	return err
}

func (d *delugeProcess) CreateLink(name, path string) (string, error) {
	tname := fmt.Sprintf("%s.torrent", name)
	tpath := filepath.Join(d.torrentPath, tname)
	log.Printf("Creating .torrent file of '%s' at '%s'...", path, tpath)
	cmd := exec.Command("transmission-create", "-p", fmt.Sprintf("-t=http://%s%s/announce", d.ip.String(), d.trackerBind), "-o", tpath, path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("http://%s%s/%s", d.ip.String(), d.filesBind, tname), nil
}

func (d *delugeProcess) SeedLink(turl, path string) error {
	log.Printf("Adding .torrent file from url '%s' with dir '%s' to client...", turl, path)
	cmd := exec.Command("deluge-console", "add", turl, "-p", path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

//@todo implement
func (d *delugeProcess) Benchmark() (string, error) {

	//generate a quasi-large file
	f, err := ioutil.TempFile("", "cell_bench_")
	if err != nil {
		return "", err
	}

	limit := int64(100000000)
	log.Printf("created tmp file %s, filling with '%d' random bytes...", f.Name(), limit)
	_, err = io.Copy(f, io.LimitReader(rand.Reader, limit))
	if err != nil {
		return "", err
	}

	//create a .torrent file and publish
	turl, err := d.CreateLink(filepath.Base(f.Name()), f.Name())
	if err != nil {
		return "", err
	}

	//seed .torrent from
	err = d.SeedLink(turl, filepath.Dir(f.Name()))
	if err != nil {
		return turl, err
	}

	return turl, nil
}

func (d *delugeProcess) Pull(uri string) error {
	loc, err := url.Parse(uri)
	if err != nil {
		return err
	}

	cmd := exec.Command("deluge-console", "add", loc.String())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

func (d *delugeProcess) Stop() error {
	_, err := d.Process.Wait()
	if err != nil {
		return err
	}

	return nil
}
