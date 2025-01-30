package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/slcjordan/autodemo"
	"github.com/slcjordan/autodemo/logger"
)

type Worker struct {
	mu      sync.RWMutex // guards project, enabled
	project string
	enabled bool

	Addr  string
	Reset func()
}

func (w *Worker) Recording() (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.project, w.enabled
}

func fileExists(ctx context.Context, parts ...string) bool {
	dirPath := filepath.Join(parts...)
	_, err := os.Stat(dirPath)
	if err == nil {
		return true
	} else if os.IsNotExist(err) {
		return false
	}
	logger.Errorf(ctx, "error checking if file exists %q: %s", dirPath, err)
	return false
}

func (w *Worker) StartProject(ctx context.Context, name string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if fileExists(ctx, "../../projects", name) {
		return fmt.Errorf("project already exists: %q", name)
	}

	w.project = name
	w.enabled = true
	return nil
}

func (w *Worker) StopProject(ctx context.Context, desc string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.enabled = false
	go w.Reset()
	req, err := w.saveProject(ctx, autodemo.Project{
		Name:       w.project,
		WorkingDir: "/projects",
		Desc:       desc,
	})
	if err != nil {
		logger.Errorf(ctx, "could not create save project request: %s", err)
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Errorf(ctx, "could not save project: %s", err)
		return err
	}
	if (resp.StatusCode / 100) != 2 {
		logger.Errorf(ctx, "got bad status when saving project: %d %s", resp.StatusCode, resp.Status)
		return fmt.Errorf("bad status from worker backend: %d", resp.StatusCode)
	}
	return nil
}

func (w *Worker) Notify(history autodemo.History) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if !w.enabled {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := w.saveHistory(ctx, w.project, history)
	if err != nil {
		logger.Errorf(ctx, "could not create save history request: %s", err)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Errorf(ctx, "could not save history: %s", err)
		return
	}
	if (resp.StatusCode / 100) != 2 {
		logger.Errorf(ctx, "got bad status when saving history: %d %s", resp.StatusCode, resp.Status)
		return
	}
	fmt.Println("saved history")
}

func (w *Worker) saveHistory(ctx context.Context, project string, history autodemo.History) (*http.Request, error) {
	var buff bytes.Buffer
	enc := json.NewEncoder(&buff)
	err := enc.Encode(history)
	if err != nil {
		return nil, err
	}
	result, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://localhost:8080/project/%s/history", project), &buff)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (w *Worker) saveProject(ctx context.Context, project autodemo.Project) (*http.Request, error) {
	var buff bytes.Buffer
	enc := json.NewEncoder(&buff)
	err := enc.Encode(project)
	if err != nil {
		return nil, err
	}
	result, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost:8080/project", &buff)
	if err != nil {
		return nil, err
	}
	return result, nil
}
