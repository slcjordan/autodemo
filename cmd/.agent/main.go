package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/slcjordan/autodemo/logger"
	"github.com/slcjordan/autodemo/recording"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	var runner recording.Pty

	go func() {
		defer cancel()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM) // Catch SIGINT and SIGTERM
		<-sigChan
		runner.Close(ctx)
	}()

	flag.StringVar(&runner.Listen, "listen", os.Getenv("AUTODEMO_LISTEN"), "address opens a tcp socket to listen for key events")
	flag.StringVar(&runner.Outfile, "outfile", os.Getenv("AUTODEMO_OUTFILE"), "where to save the webm recording")
	flag.Parse()

	fmt.Printf("%#v\n", runner)
	err := runner.Run(ctx)
	if err != nil {
		logger.Errorf(ctx, "while running agent: %s", err)
	}
}
