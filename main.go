package main

import (
	"log"
	"net/http"
	"net/http/cgi"
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
	res := []string{"opt", "git"}
	for _, p := range parts {
		res = append(res, p)
		if strings.HasSuffix(p, ".git") {
			break
		}
	}

	//create directory and init repo if it doesn't exit yet
	repopath := string(filepath.Separator) + filepath.Join(res...)
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
}

func agentAction(c *cli.Context) {
	log.Println("Starting agent...")

	h := &cgi.Handler{
		Path: "/usr/lib/git-core/git-http-backend",
		Root: "/git/",
		Env:  []string{"GIT_PROJECT_ROOT=/opt/git", "GIT_HTTP_EXPORT_ALL=1", "REMOTE_USER=cell"},
	}

	log.Println("HTTP server listening on ':3838'...")
	err := http.ListenAndServe(":3838", &AutoCreator{h})
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
		},
	}

	app.Run(os.Args)
}
