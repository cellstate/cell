package services

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Storage interface {
	Start() error
	Stop() error
}

func NewGitServer(exchange Exchange, gossip Gossip, ip net.IP) (*gitServer, error) {
	h := &cgi.Handler{
		Path: "/usr/lib/git-core/git-http-backend",
		Root: "/git/",
		Env:  []string{"GIT_PROJECT_ROOT=/tmp", "GIT_HTTP_EXPORT_ALL=1", "REMOTE_USER=cell"},
	}

	return &gitServer{
		exchange: exchange,
		gossip:   gossip,
		port:     3838,
		ip:       ip,
		cgih:     h,
	}, nil
}

type gitServer struct {
	exchange Exchange
	gossip   Gossip
	port     int
	ip       net.IP
	cgih     *cgi.Handler
}

func (ac *gitServer) Stop() error {
	return nil
}

func (ac *gitServer) Start() error {
	go func() {
		bind := fmt.Sprintf("%s:%d", ac.ip.String(), ac.port)
		log.Printf("HTTP server listening on '%s'...", bind)
		log.Printf("REMOTE: 'git remote add origin http://%s/test.git'", bind)
		err := http.ListenAndServe(bind, ac)
		if err != nil {
			log.Fatal(err)
		}
	}()

	return nil
}

func (ac *gitServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	res := []string{}
	for _, p := range parts {
		if p == "" {
			continue
		}
		res = append(res, p)
		if strings.HasSuffix(p, ".git") {
			break
		}
	}

	name := strings.Join(res, "")

	//create directory and init repo if it doesn't exit yet
	repopath := string(filepath.Separator) + filepath.Join("tmp", name)

	log.Printf("we got res '%s', and name '%s' and repopath '%s'", res, name, repopath)
	if _, err := os.Stat(repopath); os.IsNotExist(err) {

		err := os.MkdirAll(repopath, 0777)
		if err != nil {
			log.Printf("Failed to create directory path '%s': '%s'", repopath, err)
		}

		cmd := exec.Command("git", "--bare", "init")
		cmd.Dir = repopath
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			log.Printf("Failed init bare repo: '%s'", err)
		}
	}

	//let git cgi script handle the actual file writing
	ac.cgih.ServeHTTP(w, r)

	//when it was a post with git receive, emit a new torrent to be downloaded
	if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "git-receive-pack") {
		log.Printf("Detected new git commits: %s %s, emitting event...", r.Method, r.URL.String())

		//create torrent file and publish on fileserver
		turl, err := ac.exchange.CreateLink(name, repopath)
		if err != nil {
			log.Printf("Failed to create exchange link for repopath '%s': %s", repopath, err)
			return
		}

		//start seeding newly created torrent
		log.Printf("Got new link: %s, start seeding", turl)
		err = ac.exchange.SeedLink(turl, filepath.Dir(repopath))
		if err != nil {
			log.Printf("Failed to start seeding '%s': %s", turl, err)
			return
		}

		//gossip new torrent
		log.Printf("Gossip new torrent...")
		err = ac.gossip.EmitTorrent(turl)
		if err != nil {
			log.Fatalf("Failed to gossip torrent url '%s': %s", turl, err)
		}
	}
}
