package main

import (
	"log"
	"os"

	"github.com/codegangsta/cli"
)

func main() {
	cli.NewApp().Run(os.Args)

	log.Println("Hello world!")
}
