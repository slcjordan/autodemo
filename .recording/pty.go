package recording

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/slcjordan/autodemo/logger"
)

var Stdout io.Writer = os.Stdout
var Stderr io.Writer = os.Stderr

// intended to be used inside screen-capture docker
type Pty struct {
	ffmpeg *exec.Cmd

	Listen  string
	Outfile string
}

func ptyList(ctx context.Context) []string {
	files, err := os.ReadDir("/dev/pts")
	if err != nil {
		logger.Errorf(ctx, "could not list pty:", err)
		return nil
	}

	var result []string
	for _, file := range files {
		result = append(result, "/dev/pts/"+file.Name())
	}
	return result
}

func firstNewPty(before []string, after []string) string {
outer:
	for _, a := range after {
		for _, b := range before {
			if a == b {
				continue outer
			}
		}
		return a
	}
	return ""
}

func (p *Pty) Run(ctx context.Context) error {
	err := p.startXvfb(ctx)
	if err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	before := ptyList(ctx)
	err = p.startXTerm(ctx)
	if err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	fmt.Println("starting screen capture")
	err = p.startScreenCapture(ctx)
	if err != nil {
		return err
	}
	after := ptyList(ctx)
	defer p.Close(ctx)

	fmt.Println("starting stream")
	return p.stream(ctx, firstNewPty(before, after))
}

func (p *Pty) Close(ctx context.Context) error {
	fmt.Println("stopping screen capture")
	err := p.stopScreenCapture(ctx)
	time.Sleep(3 * time.Second)
	return err
}

func (p *Pty) startXvfb(ctx context.Context) error {
	xvfb := exec.CommandContext(
		ctx,
		"Xvfb", ":99", "-screen", "0", "1280x720x24",
	)
	xvfb.Stdout = Stdout
	xvfb.Stderr = Stderr
	return xvfb.Start()
}

func (p *Pty) startXTerm(ctx context.Context) error {
	xterm := exec.CommandContext(
		ctx,
		"xterm", "-geometry", "80x24", "-fa", "'Monospace'", "-fs", "12",
	)
	xterm.Stdout = Stdout
	xterm.Stderr = Stderr
	return xterm.Start()
}

func (p *Pty) startScreenCapture(ctx context.Context) error {
	p.ffmpeg = exec.CommandContext(
		ctx,
		"ffmpeg", "-y", "-video_size", "1280x720", "-framerate", "30", "-f", "x11grab", "-i", ":99", "-c:v", "libvpx-vp9", "-preset", "slow", "-c:a", "libopus", "-crf", "30", p.Outfile,
	)
	pr, pw := io.Pipe()
	stderr := io.TeeReader(pr, Stderr)
	p.ffmpeg.Stdout = Stdout
	p.ffmpeg.Stderr = pw

	err := p.ffmpeg.Start()
	buff := make([]byte, 2500, 2500)
	io.ReadAtLeast(stderr, buff, 2500) // recording has started
	go io.Copy(io.Discard, stderr)     // keep the tee running
	return err
}

func (p *Pty) stopScreenCapture(ctx context.Context) error {
	fmt.Println("stopping screen capture")
	err := p.ffmpeg.Process.Signal(os.Interrupt)
	if err != nil {
		logger.Errorf(ctx, "could not stop ffmpeg: %s", err)
		p.ffmpeg.Process.Kill()
	}
	return p.ffmpeg.Wait()
}

func (p *Pty) stream(ctx context.Context, ptyfile string) error {
	listener, err := net.Listen("tcp", p.Listen)
	if err != nil {
		fmt.Println("Error starting server:", err)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Println("opening", ptyfile)
	pty, err := os.OpenFile(ptyfile, os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer pty.Close()

	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()
	fmt.Println("connected")
	pr, pw := io.Pipe()
	reader := io.TeeReader(conn, pw)
	go io.Copy(os.Stdout, pr)
	_, err = io.Copy(pty, reader)
	if err != nil {
		return err
	}

	fmt.Println("hitting return")
	hitReturn := exec.CommandContext(
		ctx,
		"xdotool", "key", "Return",
	)
	hitReturn.Stdout = Stdout
	hitReturn.Stderr = Stderr
	return hitReturn.Run()
}
