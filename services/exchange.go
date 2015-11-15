package services

import (
	"crypto/rand"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
)

func NewDeluge(ip net.IP) (Exchange, error) {
	return &delugeProcess{ip: ip}, nil
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
	cmd := exec.Command("deluged", "-d", "-L", "debug", "-i", d.ip.String())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	if err != nil {
		return err
	}

	d.Process = cmd.Process
	return nil
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

	//create a private torrent file for it, use a public tracker for announce (transmission-create)
	tpath := f.Name() + ".torrent"
	log.Printf("Creating .torrent file of '%s' ('%d' bytes) at '%s'...", f.Name(), size, tpath)
	cmd := exec.Command("transmission-create", "-p", "-t=udp://tracker.openbittorrent.com:80", "-o", tpath, f.Name())
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
	log.Printf("Publishing torrent files on '%s'", bind)
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
