package commands

import (
	"bytes"
	"io"
	"log"
	"os"
	"os/exec"

	"github.com/codegangsta/cli"
)

var Pull = cli.Command{
	Name:  "pull",
	Usage: "...",
	Flags: []cli.Flag{},
	Action: func(c *cli.Context) {
		buff := bytes.NewBuffer(nil)
		_, err := io.Copy(buff, os.Stdin)
		if err != nil {
			log.Fatalf("failed to read Stdin: %s", err)
		}

		//there is a new torrent in the network
		//@todo how to determine the location of the torrent directory
		turl := buff.String()
		tdir := "/tmp"

		log.Printf("Adding .torrent file from url '%s' with dir '%s' to client...", turl, tdir)
		cmd := exec.Command("deluge-console", "add", turl, "-p", tdir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			log.Fatal(err)
		}
	},
}
