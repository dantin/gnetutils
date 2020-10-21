package main

import (
	"log"
	"os"

	"github.com/dantin/gnetutils/client/load"
)

func main() {
	app := load.New(os.Args)
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
