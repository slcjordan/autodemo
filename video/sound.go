package video

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/slcjordan/autodemo/logger"
)

type SoundFile struct {
	filename string
	duration time.Duration
}

func load(ctx context.Context, filename string) (SoundFile, error) {
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, "ffprobe", filename, "-show_entries", "format=duration")
	cmd.Stdout = &stdout
	cmd.Stderr = Stderr
	err := cmd.Run()
	if err != nil {
		return SoundFile{}, err
	}
	tags := strings.Split(stdout.String(), "FORMAT]")
	if len(tags) != 3 {
		return SoundFile{}, fmt.Errorf("unexpected number of FORMAT tags: %d", len(tags))
	}
	lines := strings.Split(tags[1], "\n")
	if len(lines) != 3 {
		return SoundFile{}, fmt.Errorf("unexpected number of FORMAT lines: %d", len(lines))
	}
	sides := strings.Split(strings.TrimSpace(lines[1]), "=")
	if len(sides) != 2 {
		return SoundFile{}, fmt.Errorf("unexpected number of FORMAT sides: %d", len(sides))
	}
	seconds, err := strconv.ParseFloat(sides[1], 64)
	if err != nil {
		return SoundFile{}, fmt.Errorf("could not parse seconds: %d", err)
	}
	return SoundFile{
		filename: filename,
		duration: time.Duration(seconds * float64(time.Second)),
	}, nil
}

type KeyboardClicks struct {
	Sounds []SoundFile
	start  time.Time
	filter *bytes.Buffer
	idx    int
}

func NewKeyboardClicks(ctx context.Context, dir string) *KeyboardClicks {
	var k KeyboardClicks
	k.filter = bytes.NewBuffer(nil)
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Fatal(err)
	}

	for _, entry := range entries {
		filename := filepath.Join(dir, entry.Name())
		sound, err := load(ctx, filename)
		if err != nil {
			logger.Errorf(ctx, "could not load %q: %s", filename, err)
			continue
		}
		if sound.duration < 60*time.Millisecond || sound.duration > 300*time.Millisecond {
			continue
		}
		k.Sounds = append(k.Sounds, sound)
	}
	return &k
}

func (k *KeyboardClicks) String() string {
	return k.filter.String()
}

func (k *KeyboardClicks) Click() {
	i := rand.Intn(len(k.Sounds))
	soundStart := (time.Now().Sub(k.start) - k.Sounds[i].duration).Milliseconds()
	fmt.Fprintf(k.filter, "[%d:a]adelay='%d|%d'[L%d];", i, soundStart, soundStart, k.idx)
	k.idx++
}

func (k *KeyboardClicks) Start() {
	k.start = time.Now()
	k.filter = bytes.NewBuffer(nil)
	k.idx = 0
}

func (k *KeyboardClicks) Stop() {
	for i := 0; i < k.idx; i++ {
		fmt.Fprintf(k.filter, "[L%d]", i)
	}
	fmt.Fprintf(k.filter, "amix=inputs=%d:normalize=0:duration=longest,volume=1.0,apad=pad_dur=15", k.idx)
}
