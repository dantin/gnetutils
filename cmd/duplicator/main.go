package main

import (
	"log"
	"os"

	"github.com/dantin/gnetutils/server/duplicator"
)

func main() {
	app := duplicator.New(os.Args)
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
