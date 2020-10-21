package main

import (
	"log"
	"os"

	"github.com/dantin/gnetutils/server/echo"
)

func main() {
	app := echo.New(os.Args)
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
