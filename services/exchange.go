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

func NewDeluge(ip net.IP) (Exchange, error) {
	return &delugeProcess{ip: ip}, nil
}

type peer struct {
	PeerID string `bencode:"peer id"`
	IP     string `bencode:"ip"`
	Port   string `bencode:"port"`
}

type Exchange interface {
	Start() error
	Stop() error

	Benchmark() error

	Pull(id string) error
}

type delugeProcess struct {
	ip net.IP
	*os.Process
}

func (d *delugeProcess) Start() error {
	cmd := exec.Command("deluged", "-d", "-L", "info", "-i", d.ip.String())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	if err != nil {
		return err
	}

	d.Process = cmd.Process
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

//@todo implement
func (d *delugeProcess) Benchmark() error {

	//generate a quasi-large file
	f, err := ioutil.TempFile("", "cell_bench_")
	if err != nil {
		return err
	}

	limit := int64(100000000)
	log.Printf("created tmp file %s, filling with '%d' random bytes...", f.Name(), limit)
	size, err := io.Copy(f, io.LimitReader(rand.Reader, limit))
	if err != nil {
		return err
	}

	tbind := ":9000"
	go func() {

		torrents := map[string]map[string]peer{}

		log.Printf("Starting tracker on '%s'...", tbind)
		err = http.ListenAndServe(tbind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

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

	//create a private torrent file for it, use a public tracker for announce (transmission-create)
	tpath := f.Name() + ".torrent"
	log.Printf("Creating .torrent file of '%s' ('%d' bytes) at '%s'...", f.Name(), size, tpath)
	cmd := exec.Command("transmission-create", "-p", fmt.Sprintf("-t=http://%s%s/announce", d.ip.String(), tbind), "-o", tpath, f.Name())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return err
	}

	//add to a torrent client, announcing itself to the tracker
	dir := filepath.Dir(f.Name())
	log.Printf("Adding .torrent file '%s' with dir '%s' to client...", tpath, dir)
	cmd = exec.Command("deluge-console", "add", tpath, "-p", dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return err
	}

	//publish directory with torrent file on a simple file server
	bind := ":8080"
	log.Printf("Publishing torrent file at: 'http://%s%s/%s'", d.ip.String(), bind, filepath.Base(tpath))
	go func() {
		log.Fatal(http.ListenAndServe(bind, http.FileServer(http.Dir(dir))))
	}()

	//make torrent available on a url

	//publish availability of new torrent file using gossip

	//anyone is able to download the torrent file from the private url

	return nil
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
