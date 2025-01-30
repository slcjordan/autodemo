package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/slcjordan/manualqa"
	"github.com/slcjordan/manualqa/logger"
	"github.com/slcjordan/manualqa/visualize"
)

func main() {
	ctx := context.Background()
	var soundFiles []manualqa.SoundFile
	for i := 0; i < 115; i++ {
		filename := fmt.Sprintf("../../assets/typing_%03d.mp3", i)
		file, err := visualize.Load(ctx, filename)
		if err != nil {
			logger.Errorf(ctx, "could not load file %q: %s", filename, err)
			continue
		}
		if file.Duration > 150*time.Millisecond && file.Duration < 350*time.Millisecond {
			soundFiles = append(soundFiles, file)
		}
	}

	command := visualize.SimulateTyping(ctx, manualqa.TypingSegment{
		LeadIn:             time.Second,
		LeadOut:            5 * time.Second,
		KeypressMinLatency: 100 * time.Millisecond,
		KeypressMaxLatency: 250 * time.Millisecond,
		Input: []byte(
			"curl https://dcone.cluster.local --insecure\n",
		),
	})
	exit := bytes.NewReader([]byte{0x04})
	reader1 := io.MultiReader(command, exit)
	/*
		reader1, in := io.Pipe()
		reader2 := io.TeeReader(io.MultiReader(command, exit), in)
	*/

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer fmt.Println("done with keypress")
		defer wg.Done()
		err := visualize.KeypressSoundFile(ctx, "keypresses.mp3", soundFiles, reader1)
		if err != nil {
			logger.Errorf(ctx, "could not create keypress sound file: %s", err)
		}
	}()
	/*
		go func() {
			defer fmt.Println("done with running")
			defer wg.Done()
			err := visualize.Run(ctx, "run.gif", reader2)
			if err != nil {
				logger.Errorf(ctx, "could not create asciinema gif file: %s", err)
			}
		}()
	*/
	wg.Wait()
}
