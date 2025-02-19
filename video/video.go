package video

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/slcjordan/autodemo"
	"github.com/slcjordan/autodemo/db"
	"github.com/slcjordan/autodemo/logger"
)

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

var Stdout io.Writer = os.Stdout
var Stderr io.Writer = os.Stderr

type Worker struct {
	db      *db.Conn
	display string
	pty     string
	env     []string
	clicks  *KeyboardClicks
}

func untilAtLeastNWritten(w io.Writer, n int) (io.Writer, chan struct{}) {
	pr, pw := io.Pipe()
	r := io.TeeReader(pr, w)
	done := make(chan struct{})
	go func() {
		buff := make([]byte, n)
		io.ReadAtLeast(r, buff, n)
		close(done)
		io.Copy(io.Discard, r)
	}()

	return pw, done
}

func NewWorker(ctx context.Context, conn *db.Conn, clicks *KeyboardClicks, disp uint) (*Worker, error) {
	display := fmt.Sprintf(":%d", disp)
	env := append(os.Environ(), fmt.Sprintf("DISPLAY=:%d", disp))
	var done chan struct{}

	xvfb := exec.CommandContext(ctx, "Xvfb", display, "-screen", "0", "1280x720x24")
	xvfb.Stdout = Stdout
	xvfb.Stderr, done = untilAtLeastNWritten(Stderr, 1172)
	xvfb.Env = env
	err := xvfb.Start()
	if err != nil {
		return nil, err
	}
	<-done

	before := ptyList(ctx)
	// xterm := exec.CommandContext(ctx, "xterm", "-geometry", "80x24", "-fa", "'Monospace'", "-fs", "12", "-name", "autodemo")
	xterm := exec.CommandContext(ctx, "xterm", "-fa", "'Monospace'", "-fs", "12", "-name", "autodemo", "-maximized", "-bg", "black", "-fg", "white")
	xterm.Stdout = Stdout
	xterm.Stderr = Stderr
	xterm.Env = env
	err = xterm.Start()
	time.Sleep(2 * time.Second)
	if err != nil {
		return nil, err
	}
	after := ptyList(ctx)
	var diff string
outer:
	for _, a := range after {
		for _, b := range before {
			if a == b || a == "/dev/pts/ptmx" {
				continue outer
			}
		}
		diff = a
		break
	}
	return &Worker{
		db:      conn,
		display: display,
		pty:     diff,
		env:     env,
		clicks:  clicks,
	}, nil
}

// var minBackoff = time.Millisecond
var minBackoff = time.Second
var maxBackoff = 15 * time.Second

func backoff(last time.Duration, retries int64) time.Duration {
	if retries == 0 {
		return minBackoff
	}
	expDelay := last << 1
	jitter := time.Duration(rand.Int63n(int64(expDelay / 2))) // Random up to 50% of delay
	if maxBackoff < expDelay+jitter {
		return maxBackoff
	}
	return expDelay + jitter
}

func (w *Worker) saveClicks(ctx context.Context, filename string) {
	ffmpeg := exec.CommandContext(ctx, "ffmpeg")
	for _, sound := range w.clicks.Sounds {
		ffmpeg.Args = append(ffmpeg.Args, "-i", sound.filename)
	}
	ffmpeg.Args = append(ffmpeg.Args, "-filter_complex", w.clicks.String())
	ffmpeg.Args = append(ffmpeg.Args, "-y", filename)
	ffmpeg.Env = w.env
	ffmpeg.Stdout = Stdout
	ffmpeg.Stderr = Stderr
	err := ffmpeg.Run()
	if err != nil {
		logger.Errorf(ctx, "could not save clicks: %s", err)
	}
}

func (w *Worker) concat(ctx context.Context, project autodemo.Project) error {
	files, err := os.ReadDir(filepath.Join(project.WorkingDir, project.Name))
	if err != nil {
		return err
	}
	var inputs []string
	var descs []string
	for _, v := range files {
		if !strings.HasPrefix(v.Name(), "clip-") {
			continue
		}
		inputs = append(inputs, v.Name())
		descs = append(descs, "desc-"+v.Name()[5:len(v.Name())-5]+".md")
	}
	inputs = sort.StringSlice(inputs)

	fileList := filepath.Join(project.WorkingDir, project.Name, "file-list.txt")
	file, err := os.OpenFile(
		fileList,
		os.O_TRUNC|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return err
	}
	combinedMD := filepath.Join(project.WorkingDir, project.Name, "combined.md")
	md, err := os.OpenFile(
		combinedMD,
		os.O_TRUNC|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return err
	}
	fmt.Fprintf(md, "#%s\n", project.Name)
	fmt.Fprintf(md, "%s\n\n", project.Desc)
	for i, input := range inputs {
		if i > 0 {
			fmt.Fprintf(file, "\n")
		}
		fmt.Fprintf(file, "file '%s'", filepath.Join(project.WorkingDir, project.Name, input))
		script, err := os.Open(filepath.Join(project.WorkingDir, project.Name, strings.Replace(descs[i], "desc-", "script-", 1)))
		if err != nil {
			fmt.Println(filepath.Join(project.WorkingDir, project.Name, descs[i]), err)
			continue
		}
		io.Copy(md, script)
		script.Close()
		fmt.Fprintf(md, "\n\n")
		src, err := os.Open(filepath.Join(project.WorkingDir, project.Name, descs[i]))
		if err != nil {
			fmt.Println(filepath.Join(project.WorkingDir, project.Name, descs[i]), err)
			continue
		}
		io.Copy(md, src)
		src.Close()
	}
	md.Close()
	file.Close()

	ffmpeg := exec.CommandContext(ctx, "ffmpeg")
	ffmpeg.Args = append(ffmpeg.Args, "-f", "concat")
	ffmpeg.Args = append(ffmpeg.Args, "-safe", "0")
	ffmpeg.Args = append(ffmpeg.Args, "-i", fileList)
	ffmpeg.Args = append(ffmpeg.Args, "-c", "copy")
	ffmpeg.Args = append(ffmpeg.Args, "-y", filepath.Join(project.WorkingDir, project.Name, "combined.webm"))
	ffmpeg.Env = w.env
	ffmpeg.Stdout = Stdout
	ffmpeg.Stderr = Stderr
	err = ffmpeg.Run()
	if err != nil {
		logger.Errorf(ctx, "could not concat output: %s", err)
		return err
	}

	lengthen := exec.CommandContext(ctx, "ffmpeg")
	lengthen.Args = append(lengthen.Args, "-i", filepath.Join(project.WorkingDir, project.Name, "combined.webm"))
	lengthen.Args = append(lengthen.Args, "-vf", "tpad=stop_mode=clone:stop_duration=10")
	lengthen.Args = append(lengthen.Args, "-c:v", "libvpx-vp9", "-c:a", "libopus")
	lengthen.Args = append(lengthen.Args, "-y", filepath.Join(project.WorkingDir, project.Name, "combined-longer.webm"))
	lengthen.Env = w.env
	lengthen.Stdout = Stdout
	lengthen.Stderr = Stderr
	err = lengthen.Run()
	if err != nil {
		logger.Errorf(ctx, "could not concat output: %s", err)
		return err
	}

	merge := exec.CommandContext(ctx, "ffmpeg")
	merge.Args = append(lengthen.Args, "-i", filepath.Join(project.WorkingDir, project.Name, "combined-longer.webm"))
	merge.Args = append(lengthen.Args, "-i", "/assets/music/vibing_over_venus.mp3")
	merge.Args = append(merge.Args, "-filter_complex", "[0:a]volume=2.5[a1];[a1]apad=pad_dur=6[a1ext];[1:a]volume=0.1[a2];[a1ext][a2]amix=inputs=2:duration=shortest[aout]")
	merge.Args = append(merge.Args, "-map", "0:v")
	merge.Args = append(merge.Args, "-map", "[aout]")
	merge.Args = append(merge.Args, "-c:v", "copy")
	merge.Args = append(merge.Args, "-c:a", "libopus")
	merge.Args = append(merge.Args, "-y", filepath.Join(project.WorkingDir, project.Name, "combined-with-music.webm"))
	merge.Env = w.env
	merge.Stdout = Stdout
	merge.Stderr = Stderr
	err = merge.Run()
	if err != nil {
		logger.Errorf(ctx, "could not add background music: %s", err)
		return err
	}

	info, err := load(ctx, filepath.Join(project.WorkingDir, project.Name, "combined-with-music.webm"))
	if err != nil {
		logger.Errorf(ctx, "could not get file info: %s", err)
		return err
	}

	// ffmpeg -i input.mp4 -filter_complex "[0:v]fade=t=out:st=END_TIME:d=3[vout];[0:a]afade=t=out:st=END_TIME:d=3[aout]" -map "[vout]" -map "[aout]" -c:v libx264 -c:a aac -b:a 192k -preset fast output.mp4
	fade := exec.CommandContext(ctx, "ffmpeg")
	fade.Args = append(fade.Args, "-i", filepath.Join(project.WorkingDir, project.Name, "combined-with-music.webm"))
	startTime := int(info.duration.Seconds() - 3)
	fade.Args = append(fade.Args, "-filter_complex", fmt.Sprintf("[0:v]fade=t=out:st=%d:d=3[vout];[0:a]afade=t=out:st=%d:d=3[aout]", startTime, startTime))
	fade.Args = append(fade.Args, "-map", "[vout]")
	fade.Args = append(fade.Args, "-map", "[aout]")
	fade.Args = append(fade.Args, "-c:v", "libvpx-vp9", "-c:a", "libopus")
	fade.Args = append(fade.Args, "-y", filepath.Join(project.WorkingDir, project.Name, "combined-with-fade.webm"))
	fade.Stdout = Stdout
	fade.Stderr = Stderr
	err = fade.Run()
	if err != nil {
		logger.Errorf(ctx, "could not fade: %s", err)
	}
	fmt.Println(fade.String())
	return err
}

func (w *Worker) mix(ctx context.Context, outfile string, videofile string, audiofiles ...string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", videofile)
	for _, f := range audiofiles {
		cmd.Args = append(cmd.Args, "-i", f)
	}
	var filter bytes.Buffer
	fmt.Fprintf(&filter, "[0:v]split[v1][v2];")
	fmt.Fprintf(&filter, "[v2]trim=start_frame=999,setpts=PTS-STARTPTS,loop=1000:1:0[vloop];")
	fmt.Fprintf(&filter, "[v1][vloop]concat=n=2:v=1:a=0[vout];")
	for i := range audiofiles {
		fmt.Fprintf(&filter, "[%d:a]", i+1)
	}
	fmt.Fprintf(&filter, "amix=inputs=%d:duration=longest[aout]", len(audiofiles))
	cmd.Args = append(cmd.Args, "-filter_complex", filter.String())
	cmd.Args = append(cmd.Args, "-map", "[vout]")
	cmd.Args = append(cmd.Args, "-map", "[aout]")
	cmd.Args = append(cmd.Args, "-c:v", "libvpx-vp9", "-c:a", "libopus", outfile)
	cmd.Stdout = Stdout
	cmd.Stderr = Stderr
	return cmd.Run()
}

func (w *Worker) runHistory(ctx context.Context, project autodemo.Project, history autodemo.History) error {
	file, err := os.OpenFile(
		filepath.Join(project.WorkingDir, project.Name, fmt.Sprintf("desc-%03d.md", history.Index)),
		os.O_TRUNC|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return err
	}
	defer file.Close()
	fmt.Fprintf(file, "command %d\n------------\n\n", history.Index)
	fmt.Fprintf(file, "```bash\n$ ")

	var done chan struct{}
	ffmpeg := exec.CommandContext(
		ctx,
		"ffmpeg",
		"-video_size", "1280x720",
		"-framerate", "30",
		"-f", "x11grab",
		"-i", w.display,
		"-c:v", "libvpx-vp9",
		"-preset", "slow",
		"-c:a", "libopus",
		"-crf", "30",
		"-y", filepath.Join(project.WorkingDir, project.Name, fmt.Sprintf("history-%03d.webm", history.Index)),
	)
	ffmpeg.Env = w.env
	ffmpeg.Stdout = Stdout
	ffmpeg.Stderr, done = untilAtLeastNWritten(Stderr, 2500)
	err = ffmpeg.Start()
	if err != nil {
		return err
	}
	defer func() {
		ffmpeg.Process.Signal(os.Interrupt)
		time.Sleep(2 * time.Second)

		w.saveClicks(ctx, filepath.Join(project.WorkingDir, project.Name, fmt.Sprintf("history-%03d.mp3", history.Index)))
	}()
	<-done
	w.clicks.Start()

	pty, err := os.OpenFile(w.pty, os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	for i, arg := range history.Args {
		if i > 0 {
			w.clicks.Click()
			pty.Write([]byte(" "))
			file.Write([]byte(" "))
			if isFlag(arg) || !isFlag(history.Args[i-1]) {
				pty.Write([]byte("\\\n  "))
				file.Write([]byte("\\\n  "))
			}
		}
		w.clicks.Click()
		pty.Write([]byte(arg))
		file.Write([]byte(arg))
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(350 * time.Millisecond)
	w.clicks.Click()
	pty.Write([]byte("\n"))
	file.Write([]byte("\n"))
	w.clicks.Stop()
	time.Sleep(history.ExecTime)
	pty.Write([]byte(history.Output))
	file.Write([]byte(history.Output))
	fmt.Fprintf(file, "\n```\n\n\n")
	pty.Close()

	hitReturn := exec.CommandContext(
		ctx,
		"xdotool", "key", "Return",
	)
	hitReturn.Env = w.env
	hitReturn.Stdout = Stdout
	hitReturn.Stderr = Stderr
	err = hitReturn.Run()
	time.Sleep(100 * time.Millisecond)
	return err
}

func isFlag(arg string) bool {
	if len(arg) == 0 {
		return false
	}
	return arg[0] == '-'
}

func (w *Worker) narrateClip(ctx context.Context, filename string, text string) error {
	body := strings.NewReader(fmt.Sprintf(`
	{
		"text": %q,
		"model_id": %q,
		"voice_settings": {
			"stability": 0.5,
			"similarity_boost": 0.5
		}
	}
	`, text, "eleven_multilingual_v2"))

	url := fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s", os.Getenv("ELEVEN_VOICE_ID"))
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		log.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", os.Getenv("ELEVEN_API_KEY"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Error: received status code %d", resp.StatusCode)
	}
	audioFile, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer audioFile.Close()

	_, err = io.Copy(audioFile, resp.Body)
	if err != nil {
		return err
	}
	return nil
}

func (w *Worker) narrate(ctx context.Context, project autodemo.Project, parts []string) error {
	for i := 0; i < len(parts); i++ {
		err := os.WriteFile(
			filepath.Join(project.WorkingDir, project.Name, fmt.Sprintf("script-%03d.md", i)),
			[]byte(parts[i]),
			0644,
		)
		if err != nil {
			return err
		}

		err = w.narrateClip(
			ctx,
			filepath.Join(project.WorkingDir, project.Name, fmt.Sprintf("narration-%03d.mp3", i)),
			parts[i],
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) askChatGPT(ctx context.Context, project autodemo.Project) ([]string, error) {
	files, err := os.ReadDir(filepath.Join(project.WorkingDir, project.Name))
	if err != nil {
		return nil, err
	}
	var filenames []string
	for _, elem := range files {
		if !strings.HasPrefix(elem.Name(), "desc-") {
			continue
		}
		filenames = append(filenames, filepath.Join(project.WorkingDir, project.Name, elem.Name()))
	}
	prompt := bytes.NewBuffer([]byte(project.Desc + "\n\nTest Plan\n=========\n\n\n"))
	filenames = sort.StringSlice(filenames)
	for _, curr := range filenames {
		f, err := os.Open(curr)
		if err != nil {
			return nil, err
		}
		io.Copy(prompt, f)
		f.Close()
	}
	prompt.Write([]byte(fmt.Sprintf(`

This is a test plan for a feature in the API. I need a script to narrate a training video for the QA engineers. The script should write all acronyms uppercase as it will be narrated by elevenlabs. Each curl request has its own clip. Please explain how each step fits into the overall test plan. Format the output as JSON with a clips array, where each clip has a name and narration field. The clips array must be length %d. Respond only with a valid JSON object. No text before or after.
`, len(filenames))))
	body := strings.NewReader(fmt.Sprintf(`
{
  "model": "gpt-4o-mini",
  "messages": [
    {
      "role": "user",
      "content": %q
    }
  ],
  "temperature": 0.7
}
  `, prompt.String()))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("OPENAI_API_KEY"))
	req.Header.Set("OpenAI-Organization", os.Getenv("OPENAI_API_ORG_ID"))
	req.Header.Set("OpenAI-Project", os.Getenv("OPENAI_API_PROJ_ID"))
	dump, err := httputil.DumpRequestOut(req, true)
	fmt.Println(string(dump))
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string
			}
		}
	}
	dump, err = httputil.DumpResponse(resp, true)
	if err != nil {
		return nil, err
	}
	fmt.Println(string(dump))
	dec := json.NewDecoder(resp.Body)
	err = dec.Decode(&completion)
	if err != nil {
		return nil, err
	}
	if len(completion.Choices) < 1 {
		return nil, errors.New("no choices")
	}
	content := completion.Choices[0].Message.Content
	fmt.Println("content is", content)

	var Body struct {
		Clips []struct {
			Name      string
			Narration string
		}
	}
	content = strings.TrimPrefix(content, "```json\n")
	content = strings.TrimSuffix(content, "\n```")
	err = json.Unmarshal([]byte(content), &Body)
	if err != nil {
		return nil, err
	}
	var parts []string
	for _, clip := range Body.Clips {
		parts = append(parts, clip.Narration)
	}
	return parts, nil
}

func (w *Worker) runProject(ctx context.Context, status string, project autodemo.Project) error {
	fmt.Println("runProject", status, project)
	err := os.MkdirAll(filepath.Join(project.WorkingDir, project.Name), 0755)
	if err != nil {
		return err
	}
	switch status {
	case "pending":
		defer func() {
			cls := exec.CommandContext(
				ctx,
				"xdotool", "type", "clear",
			)
			cls.Env = w.env
			cls.Stdout = Stdout
			cls.Stderr = Stderr
			err := cls.Run()
			if err != nil {
				logger.Errorf(ctx, "could not clear xterm screen: %s", err)
			}

			hitReturn := exec.CommandContext(
				ctx,
				"xdotool", "key", "Return",
			)
			hitReturn.Env = w.env
			hitReturn.Stdout = Stdout
			hitReturn.Stderr = Stderr
			err = hitReturn.Run()
			if err != nil {
				logger.Errorf(ctx, "could not hit return: %s", err)
			}
		}()
		for {
			err := w.db.DoNextHistoryJob(ctx, project, w.runHistory)
			if err != nil {
				if errors.Is(err, autodemo.IterDone) {
					logger.Infof(ctx, "sending project %q to postprocessing: %s", project.Name, err)
					return nil
				}
				logger.Infof(ctx, "project error: %s", err)
				return err
			}
		}
	case "postprocessing":
		parts, err := w.askChatGPT(ctx, project)
		if err != nil {
			logger.Errorf(ctx, "error with postprocessing project %q: %s", project.Name, err)
			return err
		}
		err = w.narrate(ctx, project, parts)
		if err != nil {
			logger.Errorf(ctx, "error with narrating clips %q: %s", project.Name, err)
			return err
		}
		for i := range parts {
			err = w.mix(ctx,
				filepath.Join(project.WorkingDir, project.Name, fmt.Sprintf("clip-%03d.webm", i)),
				filepath.Join(project.WorkingDir, project.Name, fmt.Sprintf("history-%03d.webm", i)),
				filepath.Join(project.WorkingDir, project.Name, fmt.Sprintf("history-%03d.mp3", i)),
				filepath.Join(project.WorkingDir, project.Name, fmt.Sprintf("narration-%03d.mp3", i)),
			)
			if err != nil {
				logger.Errorf(ctx, "error with narrating clips %q: %s", project.Name, err)
				return err
			}
		}
		return w.concat(ctx, project)
	}
	return fmt.Errorf("unknown status: %q", status)
}

func (w *Worker) Run(ctx context.Context) {
	retries := int64(0)
	for pause := time.Millisecond; ; pause = backoff(pause, retries) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(pause):
		}
		err := w.db.DoNextProjectJob(ctx, w.runProject)
		if err != nil {
			if !errors.Is(err, autodemo.IterDone) {
				logger.Errorf(ctx, "could not do next project: %s", err)
			}
			retries++
			logger.Infof(ctx, "retrying: %s", err)
			continue
		}
		retries = 0
	}
}
