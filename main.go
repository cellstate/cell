package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/cgi"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/codegangsta/cli"
)

type AutoCreator struct {
	cgih *cgi.Handler
}

func (ac *AutoCreator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	res := []string{}
	for _, p := range parts {
		res = append(res, p)
		if strings.HasSuffix(p, ".git") {
			break
		}
	}

	//create directory and init repo if it doesn't exit yet
	repopath := string(filepath.Separator) + filepath.Join("opt", "git", filepath.Join(res...))
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

	ac.cgih.ServeHTTP(w, r)
	if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "git-receive-pack") {
		log.Printf("Detected new git commits: %s %s, emitting event...", r.Method, r.URL.String())

		//get ip addr
		iface, err := net.InterfaceByName("eth0")
		if err != nil {
			log.Printf("Failed to get interface with name 'eth0': '%s'", err)
		}

		addrs, err := iface.Addrs()
		if err != nil {
			log.Printf("Failed to get interface addrs: '%s'", err)
		}

		cidr := addrs[0].String()
		ip, _, err := net.ParseCIDR(cidr)
		if err != nil {
			log.Fatalf("Failed to parse '%s' as CIDR: %s", cidr, err)
		}

		loc := &url.URL{}
		loc.Scheme = "http"
		loc.Host = fmt.Sprintf("%s:%s", ip.String(), "3838")
		loc.Path = strings.Join(res, "/")

		//emit actual event
		cmd := exec.Command("serf", "event", "new-commits", loc.String())
		cmd.Dir = repopath
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			log.Printf("Failed emit serf event: '%s'", err)
		}
	}
}

func replicateAction(c *cli.Context) {
	data, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("Failed to read stdin for src address: '%s'", err)
	}

	remote := strings.TrimSpace(string(data))
	loc, err := url.Parse(remote)
	if err != nil {
		log.Fatalf("Failed to parse input '%s' as url: %s", data, err)
	}

	parts := strings.Split(loc.Path, "/")
	res := []string{}
	for _, p := range parts {
		res = append(res, p)
		if strings.HasSuffix(p, ".git") {
			break
		}
	}

	//create directory and init repo if it doesn't exit yet!
	repopath := string(filepath.Separator) + filepath.Join("opt", "git", filepath.Join(res...))
	cmd := exec.Command("git", "fetch", remote)
	if _, err := os.Stat(repopath); os.IsNotExist(err) {
		err := os.MkdirAll(repopath, 0777)
		if err != nil {
			log.Printf("Failed to create directory path '%s': '%s'", repopath, err)
		}

		cmd = exec.Command("git", "clone", "--mirror", "--bare", remote, ".")
	}

	log.Printf("New commits at remote '%s', cloning to '%s'...", remote, repopath)
	cmd.Dir = repopath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		log.Printf("Failed to clone from '%s': '%s'", remote, err)
	}
}

func agentAction(c *cli.Context) {
	log.Println("Starting agent...")

	h := &cgi.Handler{
		Path: "/usr/lib/git-core/git-http-backend",
		Root: "/git/",
		Env:  []string{"GIT_PROJECT_ROOT=/opt/git", "GIT_HTTP_EXPORT_ALL=1", "REMOTE_USER=cell"},
	}

	//start serf agent
	cmd := exec.Command("serf", "agent", "-log-level=debug", "-event-handler", "user:new-commits=cell replicate")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	if err != nil {
		log.Printf("Failed to start serf agent: '%s'", err)
	}

	//want to join
	if remote := c.String("join"); remote != "" {
		log.Printf("Joining gossip via '%s...", remote)

		cmd := exec.Command("serf", "join", remote)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			log.Printf("Failed to join gossip: '%s'", err)
		}
	}

	//start http server
	log.Println("HTTP server listening on ':3838'...")
	err = http.ListenAndServe(":3838", &AutoCreator{h})
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	app := cli.NewApp()
	app.Name = "boom"
	app.Usage = "make an explosive entrance"
	app.Commands = []cli.Command{
		{
			Name:   "agent",
			Usage:  "...",
			Action: agentAction,
			Flags: []cli.Flag{
				cli.StringFlag{Name: "join", Usage: "..."},
			},
		},
		{
			Name:   "replicate",
			Usage:  "...",
			Action: replicateAction,
			Flags:  []cli.Flag{},
		},
	}

	app.Run(os.Args)
}
