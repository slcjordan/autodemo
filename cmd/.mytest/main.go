package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/slcjordan/autodemo/recording"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	term, err := recording.NewTerminalWithContext(
		ctx,
		filepath.Join("/Users/jordan.crabtree/Developer/src/github.com/slcjordan/autodemo/assets/recordings", "outdemo.webm"),
	)
	if err != nil {
		panic(err)
	}

	defer term.Close()
	fmt.Println("writing things")
	term.Write([]byte("this\n"))
	time.Sleep(2 * time.Second)
	term.Write([]byte("is working\n"))
	time.Sleep(2 * time.Second)
}
