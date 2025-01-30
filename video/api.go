package video

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/slcjordan/autodemo"
	"github.com/slcjordan/autodemo/db"
	"github.com/slcjordan/autodemo/logger"
)

type API struct {
	conn *db.Conn
	mux  *http.ServeMux
}

func NewAPI(conn *db.Conn) *API {
	api := API{
		conn: conn,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /project", api.SaveProject)
	mux.HandleFunc("POST /project/{project}/history", api.SaveHistory)
	api.mux = mux
	return &api
}

func (a *API) SaveHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var history autodemo.History
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(&history)
	if err != nil {
		logger.Infof(ctx, "could not decode project: %s", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	project := r.PathValue("project")
	a.conn.MaybeSaveHistoryJob(ctx, project, history)
}

func (a *API) SaveProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var project autodemo.Project
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(&project)
	if err != nil {
		logger.Infof(ctx, "could not decode project: %s", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dirPath := filepath.Join(project.WorkingDir, project.Name)
	if _, err := os.Stat(dirPath); err == nil {
		logger.Infof(ctx, "Directory '%s' exists\n", dirPath)
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	err = os.MkdirAll(dirPath, 0755)
	if err != nil {
		logger.Errorf(ctx, "could not create project directory: %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.conn.MaybeSaveProjectJob(ctx, project)
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mux.ServeHTTP(w, r)
}
