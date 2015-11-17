package main

import (
	"os"

	"github.com/codegangsta/cli"

	"github.com/cellstate/cell/commands"
)

func main() {
	app := cli.NewApp()
	app.Name = "boom"
	app.Usage = "make an explosive entrance"
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "token,t", Usage: "..."},
	}

	app.Commands = []cli.Command{
		commands.Join,
		commands.Pull,
	}

	app.Run(os.Args)
}
