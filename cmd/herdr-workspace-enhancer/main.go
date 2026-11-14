package main

import (
	"errors"
	"fmt"
	"herdr-workspace-enhancer/controller"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (err error) {
	c := controller.New()
	defer func() { err = errors.Join(err, c.Shutdown()) }()
	return c.Command().Execute()
}
