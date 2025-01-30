package visualize

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/slcjordan/manualqa"
	"github.com/slcjordan/manualqa/logger"
)

func randomKeypressDuration(s manualqa.TypingSegment) time.Duration {
	return s.KeypressMinLatency + time.Duration(rand.Int63n(int64(s.KeypressMaxLatency-s.KeypressMinLatency)))
}

func SimulateTyping(ctx context.Context, s manualqa.TypingSegment) io.Reader {
	out, in := io.Pipe()

	go func() {
		defer in.Close()
		for _, char := range s.Input {
			_, err := in.Write([]byte{char})
			if err != nil {
				logger.Errorf(ctx, "could not simulate keypress: %s", err)
				return
			}
			time.Sleep(randomKeypressDuration(s))
		}
	}()
	return out
}

func Load(ctx context.Context, filename string) (manualqa.SoundFile, error) {
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, "ffprobe", filename, "-show_entries", "format=duration")
	cmd.Stdout = &stdout
	err := cmd.Run()
	if err != nil {
		return manualqa.SoundFile{}, err
	}
	tags := strings.Split(stdout.String(), "FORMAT]")
	if len(tags) != 3 {
		return manualqa.SoundFile{}, fmt.Errorf("unexpected number of FORMAT tags: %d", len(tags))
	}
	lines := strings.Split(tags[1], "\n")
	if len(lines) != 3 {
		return manualqa.SoundFile{}, fmt.Errorf("unexpected number of FORMAT lines: %d", len(lines))
	}
	sides := strings.Split(strings.TrimSpace(lines[1]), "=")
	if len(sides) != 2 {
		return manualqa.SoundFile{}, fmt.Errorf("unexpected number of FORMAT sides: %d", len(sides))
	}
	seconds, err := strconv.ParseFloat(sides[1], 64)
	if err != nil {
		return manualqa.SoundFile{}, fmt.Errorf("could not parse seconds: %d", err)
	}
	return manualqa.SoundFile{
		Name:     filename,
		Duration: time.Duration(seconds * float64(time.Second)),
	}, nil
}

type keypressEvent struct {
	keypresses int
	marker     time.Duration
}

func KeypressSoundFile(ctx context.Context, dest string, soundFiles []manualqa.SoundFile, r io.Reader) error {
	start := time.Now()
	buffer := make([]byte, 1024)
	var timeline []keypressEvent
	var event keypressEvent
	fmt.Println("keypress 1")

	for {
		n, err := r.Read(buffer)
		if err != nil {
			if errors.Is(err, io.EOF) {
				timeline = append(timeline, event)
				fmt.Println("keypress break")
				break
			}
			return err
		}
		marker := time.Now().Sub(start)
		if marker-event.marker > 100*time.Millisecond {
			timeline = append(timeline, event)
			event = keypressEvent{
				keypresses: n,
				marker:     marker,
			}
		} else {
			event.keypresses += n
		}
	}

	cmd := exec.CommandContext(ctx, "ffmpeg")
	var debug bytes.Buffer
	cmd.Stdout = &debug
	cmd.Stderr = &debug
	for _, f := range soundFiles {
		cmd.Args = append(cmd.Args, "-i")
		cmd.Args = append(cmd.Args, f.Name)
	}
	cmd.Args = append(cmd.Args, "-filter_complex")

	var filter strings.Builder
	var labels []string

	fmt.Println("keypress 2")
	for i, t := range timeline {
		idx := rand.Intn(len(soundFiles))
		sound := soundFiles[idx]
		if sound.Duration > t.marker {
			continue
		}
		soundStart := (t.marker - sound.Duration).Milliseconds()
		filter.WriteString(
			fmt.Sprintf("[%d:a]adelay=%d:%d[L%d];", idx, soundStart, soundStart, i),
		)
		labels = append(labels, fmt.Sprintf("[L%d]", i))
		if t.keypresses < 2 {
			continue
		}

		// add another sound in if there was more than one keypress (like it's a key combination)
		t.marker -= sound.Duration
		idx = rand.Intn(len(soundFiles))
		sound = soundFiles[idx]
		if (sound.Duration / 2) > t.marker {
			continue
		}
		soundStart = (t.marker - (sound.Duration / 2)).Milliseconds()
		filter.WriteString(
			fmt.Sprintf("[%d:a]adelay'=%d|%d'[L%d_leader];", idx, soundStart, soundStart, i),
		)
		labels = append(labels, fmt.Sprintf("[L%d_leader]", i))
	}
	filter.WriteString(
		fmt.Sprintf("%samix=inputs=%d:duration=longest", strings.Join(labels, ""), len(labels)),
	)

	cmd.Args = append(cmd.Args, filter.String())
	cmd.Args = append(cmd.Args, dest)
	fmt.Println("keypress 3")
	err := cmd.Run()
	if err != nil {
		fmt.Println(cmd.String(), debug.String())
		return err
	}
	return nil
}

func Run(ctx context.Context, filename string, r io.Reader) (resultErr error) {
	parent := exec.CommandContext(ctx, "asciinema", "rec", filename)
	parent.Stdout = os.Stdout
	parent.Stderr = os.Stderr

	stdin, err := parent.StdinPipe()
	defer stdin.Close()
	if err != nil {
		logger.Errorf(ctx, "could not create stdin pipe: %s", err)
	}
	err = parent.Start()
	if err != nil {
		return err
	}
	defer func() {
		err := parent.Wait()
		if err != nil && resultErr == nil {
			resultErr = err
		}
	}()

	_, err = io.Copy(stdin, r)
	if err != nil {
		return err
	}
	return nil
}
