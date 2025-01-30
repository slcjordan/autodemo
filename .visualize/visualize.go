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
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/slcjordan/autodemo"
	"github.com/slcjordan/autodemo/logger"
)

var Stdout io.Writer = os.Stdout
var Stderr io.Writer = os.Stderr

func randomKeypressDuration(s autodemo.TypingSegment) time.Duration {
	return s.KeypressMinLatency + time.Duration(rand.Int63n(int64(s.KeypressMaxLatency-s.KeypressMinLatency)))
}

func SimulateTyping(ctx context.Context, s autodemo.TypingSegment) io.Reader {
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

func Load(ctx context.Context, filename string) (autodemo.SoundFile, error) {
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, "ffprobe", filename, "-show_entries", "format=duration")
	cmd.Stdout = &stdout
	cmd.Stderr = Stderr
	err := cmd.Run()
	if err != nil {
		return autodemo.SoundFile{}, err
	}
	tags := strings.Split(stdout.String(), "FORMAT]")
	if len(tags) != 3 {
		return autodemo.SoundFile{}, fmt.Errorf("unexpected number of FORMAT tags: %d", len(tags))
	}
	lines := strings.Split(tags[1], "\n")
	if len(lines) != 3 {
		return autodemo.SoundFile{}, fmt.Errorf("unexpected number of FORMAT lines: %d", len(lines))
	}
	sides := strings.Split(strings.TrimSpace(lines[1]), "=")
	if len(sides) != 2 {
		return autodemo.SoundFile{}, fmt.Errorf("unexpected number of FORMAT sides: %d", len(sides))
	}
	seconds, err := strconv.ParseFloat(sides[1], 64)
	if err != nil {
		return autodemo.SoundFile{}, fmt.Errorf("could not parse seconds: %d", err)
	}
	return autodemo.SoundFile{
		Name:     filename,
		Duration: time.Duration(seconds * float64(time.Second)),
	}, nil
}

type keypressEvent struct {
	keypresses int
	marker     time.Duration
}

func KeypressSoundFile(ctx context.Context, dest string, soundFiles []autodemo.SoundFile, r io.Reader) error {
	start := time.Now()
	buffer := make([]byte, 1024)
	var timeline []keypressEvent
	var event keypressEvent

	for {
		n, err := r.Read(buffer)
		if err != nil {
			if errors.Is(err, io.EOF) {
				timeline = append(timeline, event)
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

	for i, t := range timeline {
		idx := rand.Intn(len(soundFiles))
		sound := soundFiles[idx]
		if sound.Duration > t.marker {
			continue
		}
		soundStart := (t.marker - sound.Duration).Milliseconds()
		filter.WriteString(
			fmt.Sprintf("[%d:a]adelay='%d|%d'[L%d];", idx, soundStart, soundStart, i),
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
		fmt.Sprintf("%samix=inputs=%d:normalize=0:duration=longest,volume=8.0,apad=pad_dur=15", strings.Join(labels, ""), len(labels)),
	)

	cmd.Args = append(cmd.Args, filter.String())
	cmd.Args = append(cmd.Args, dest)
	err := cmd.Run()
	if err != nil {
		logger.Errorf(ctx, cmd.String()+"\n"+debug.String())
		return err
	}
	return nil
}

func Asciinema(ctx context.Context, filename string, r io.Reader) (string, error) {
	//  create asciinema cast file
	castfile := filepath.Join(os.TempDir(), "session-"+time.Now().Format("200601021504")+".cast")
	defer os.Remove(castfile)
	outfile := filepath.Join(os.TempDir(), "session-"+time.Now().Format("200601021504")+".txt")
	defer os.Remove(outfile)
	cmd := exec.CommandContext(ctx, "asciinema", "rec", "-c", fmt.Sprintf("bash | tee %s", outfile), castfile)
	cmd.Stdout = Stdout
	cmd.Stderr = Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		logger.Errorf(ctx, "could not create stdin pipe: %s", err)
	}
	defer stdin.Close()
	err = cmd.Start()
	if err != nil {
		return "", err
	}
	_, err = io.Copy(stdin, r)
	if err != nil {
		logger.Errorf(ctx, "could not copy to stdin pipe: %s", err)
	}
	err = cmd.Wait()
	if err != nil {
		logger.Errorf(ctx, "asciinema exit error: %s", err)
		return "", err
	}

	// save cast file to gif
	convert := exec.CommandContext(ctx, "agg", castfile, filename)
	convert.Stdout = Stdout
	convert.Stderr = Stderr
	err = convert.Run()
	if err != nil {
		logger.Errorf(ctx, "convert exit error: %s", err)
		return "", err
	}

	var out bytes.Buffer
	cat := exec.CommandContext(ctx, "cat", outfile)
	cat.Stdout = &out
	cat.Stderr = Stderr
	err = cat.Run()
	if err != nil {
		logger.Errorf(ctx, "convert exit error: %s", err)
		return "", err
	}
	return out.String(), err
}

func Mix(ctx context.Context, outfile string, videofile string, audiofiles ...string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", videofile)
	for _, f := range audiofiles {
		cmd.Args = append(cmd.Args, "-i", f)
	}
	cmd.Args = append(cmd.Args, "-filter_complex", "[0:v]split[v1][v2];[v2]trim=start_frame=999,setpts=PTS-STARTPTS,loop=1000:1:0[vloop];[v1][vloop]concat=n=2:v=1:a=0[vout]")
	cmd.Args = append(cmd.Args, "-map", "[vout]")
	for i, _ := range audiofiles {
		cmd.Args = append(cmd.Args, "-map", fmt.Sprintf("%d:a", i+1))
	}
	cmd.Args = append(cmd.Args, "-c:v", "libvpx-vp9", "-c:a", "libopus", outfile)
	cmd.Stdout = Stdout
	cmd.Stderr = Stderr
	return cmd.Run()
}

type Execer struct{}

func (e *Execer) Exec(ctx context.Context, cmd string) (string, error) {
	return Asciinema(ctx, "heyo.gif", strings.NewReader("stty -echo; "+cmd+"\n stty echo\n"+string([]byte{0x04})))
}
