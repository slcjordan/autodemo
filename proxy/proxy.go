package proxy

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/slcjordan/autodemo/logger"
)

type PKIProvider interface {
	EEPrivateKey() crypto.PrivateKey
	SignCSR(der []byte) ([]byte, error)
	CACertPool() *x509.CertPool
}

type Status string

const (
	Running Status = "running"
	Stopped Status = "stopped"
)

type ProjectRecorder interface {
	StartProject(ctx context.Context, name string) error
	StopProject(ctx context.Context, desc string) error
	Recording() (string, bool)
}

type Proxy struct {
	ListenHost      string
	ListenPort      string
	ListenScheme    string
	ForwardHost     string
	ForwardPort     string
	ForwardScheme   string
	ForwardInsecure bool
}

type Project struct {
	Name  string
	Error bool
	Done  bool
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

type Manager struct {
	mu        sync.Mutex // guards servers
	servers   map[*http.Server]Status
	proxies   []Proxy
	fs        http.Handler
	projectFS http.Handler
	tmpl      *template.Template
	lastError error

	SecureTransport   http.RoundTripper
	InsecureTransport http.RoundTripper
	PKIProvider       PKIProvider
	Recorder          ProjectRecorder
}

func NewManager(secureTransport http.RoundTripper, insecureTransport http.RoundTripper, pkiProvider PKIProvider, recorder ProjectRecorder) *Manager {
	tmpl, err := template.ParseFiles("../../ui/public/pages/dashboard/index.html")
	if err != nil {
		panic(err)
	}
	return &Manager{
		servers:           make(map[*http.Server]Status),
		SecureTransport:   secureTransport,
		InsecureTransport: insecureTransport,
		PKIProvider:       pkiProvider,
		Recorder:          recorder,
		fs:                http.FileServer(http.Dir("../../ui/public")),
		projectFS:         http.StripPrefix("/projects", http.FileServer(http.Dir("../../projects"))),
		tmpl:              tmpl,
	}
}

type errorWrapper []error

func (e errorWrapper) Error() string {
	return fmt.Sprintf("%d errors", len(e))
}

func (e errorWrapper) Unwrap() []error {
	return e
}

func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		switch r.URL.Query().Get("action") {
		case "record":
			m.StartProject(w, r)
		case "stop":
			m.StopProject(w, r)
		case "proxy":
			m.HandleNewProxyRequest(w, r)
		}
		http.Redirect(w, r, "/pages/dashboard", http.StatusSeeOther)
	}
	path := r.URL.Path
	switch path {
	case "/", "/index.html":
		http.Redirect(w, r, "/pages/dashboard", http.StatusPermanentRedirect)
	case "/pages/dashboard/":
		projectDir := "../../projects"
		projectFiles, err := os.ReadDir(projectDir)
		if err != nil {
			logger.Errorf(r.Context(), "could not read projects dir %q: %s", projectDir, err)
		}
		var projects []Project
		for _, f := range projectFiles {
			if f.Name() == ".gitinclue" {
				continue
			}
			projects = append(projects, Project{
				Name:  f.Name(),
				Error: fileExists(r.Context(), projectDir, f.Name(), "error.txt"),
				Done:  fileExists(r.Context(), projectDir, f.Name(), "combined.webm"),
			})
		}
		var lastError string
		if m.lastError != nil {
			lastError = m.lastError.Error()
		}

		name, recording := m.Recorder.Recording()
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		err = m.tmpl.Execute(w, struct {
			Proxies     []Proxy
			Recording   bool
			ProjectName string
			Projects    []Project
			LastError   string
		}{
			Proxies:     m.proxies,
			Recording:   recording,
			ProjectName: name,
			Projects:    projects,
			LastError:   lastError,
		})
		if err != nil {
			logger.Errorf(r.Context(), "could not render template: %s", err)
		}
		return
	}
	if strings.HasPrefix(path, "/projects") {
		m.projectFS.ServeHTTP(w, r)
		return
	}
	m.fs.ServeHTTP(w, r)
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var shutdownMu sync.Mutex
	var shutdownErrors errorWrapper

	var wg sync.WaitGroup
	for s, _ := range m.servers {
		wg.Add(1)
		go func(s *http.Server) {
			defer wg.Done()
			err := s.Shutdown(ctx)

			if err != nil {
				shutdownMu.Lock()
				shutdownErrors = append(shutdownErrors, err)
				shutdownMu.Unlock()
			}
		}(s)
	}
	wg.Wait()
	if shutdownErrors != nil {
		return shutdownErrors
	}
	return nil // very important to return nil instead of nil slice. (see https://speakerdeck.com/campoy/understanding-nil?slide=57)
}

func (m *Manager) StartProject(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		logger.Infof(r.Context(), "could not parse http form: %s", err)
		w.WriteHeader(http.StatusBadRequest)
		m.lastError = err
		return
	}
	projectName := r.FormValue("project_name")
	err = m.Recorder.StartProject(r.Context(), projectName)
	if err != nil {
		logger.Infof(r.Context(), "could not start project: %s", err)
		w.WriteHeader(http.StatusConflict)
		m.lastError = err
		return
	}
}

func (m *Manager) StopProject(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		logger.Infof(r.Context(), "could not parse http form: %s", err)
		w.WriteHeader(http.StatusBadRequest)
		m.lastError = err
		return
	}
	desc := r.FormValue("project_desc")
	err = m.Recorder.StopProject(r.Context(), desc)
	if err != nil {
		logger.Errorf(r.Context(), "could not save project: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		m.lastError = err
		return
	}
}

func (m *Manager) HandleNewProxyRequest(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		logger.Infof(r.Context(), "could not parse http form: %s", err)
		w.WriteHeader(http.StatusBadRequest)
		m.lastError = err
		return
	}
	listenHost := r.FormValue("listen_host")
	listenPort := r.FormValue("listen_port")
	listenScheme := r.FormValue("listen_scheme")
	forwardHost := r.FormValue("forward_host")
	forwardPort := r.FormValue("forward_port")
	forwardScheme := r.FormValue("forward_scheme")
	forwardInsecure := r.FormValue("forward_insecure")

	err = m.NewProxy(listenHost, listenPort, listenScheme, forwardHost, forwardPort, forwardScheme, forwardInsecure)
	if err != nil {
		m.lastError = err
		logger.Errorf(r.Context(), "could not create new proxy: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		m.lastError = err
		return
	}
	// TODO save
}

func (m *Manager) NewProxy(listenHost, listenPort, listenScheme, forwardHost, forwardPort, forwardScheme, forwardInsecure string) error {
	proxy := httputil.NewSingleHostReverseProxy(&url.URL{
		Scheme: forwardScheme,
		Host:   forwardHost + ":" + forwardPort,
	})
	switch forwardInsecure {
	case "on":
		proxy.Transport = m.InsecureTransport
	default:
		proxy.Transport = m.SecureTransport
	}
	proxy.Director = func(r *http.Request) {
		r.URL.Scheme = forwardScheme
		r.URL.Host = forwardHost + ":" + forwardPort
		r.Host = forwardHost + ":" + forwardPort
	}
	server := http.Server{
		Addr:    "0.0.0.0:" + listenPort,
		Handler: proxy,
	}

	if listenScheme == "https" {
		csrTemplate := x509.CertificateRequest{
			Subject: pkix.Name{
				CommonName:   "localhost",
				Organization: []string{"My Server"},
			},
		}
		csr, err := x509.CreateCertificateRequest(rand.Reader, &csrTemplate, m.PKIProvider.EEPrivateKey())
		if err != nil {
			return err
		}
		cert, err := m.PKIProvider.SignCSR(csr)
		if err != nil {
			return err
		}

		tlsCert := tls.Certificate{
			PrivateKey:  m.PKIProvider.EEPrivateKey(),
			Certificate: [][]byte{cert},
		}
		server.TLSConfig = &tls.Config{
			ClientCAs:    m.PKIProvider.CACertPool(),
			Certificates: []tls.Certificate{tlsCert},
		}
	}
	m.mu.Lock()
	m.servers[&server] = Running
	m.proxies = append(m.proxies, Proxy{
		ListenHost:      listenHost,
		ListenPort:      listenPort,
		ListenScheme:    listenScheme,
		ForwardHost:     forwardHost,
		ForwardPort:     forwardPort,
		ForwardScheme:   forwardScheme,
		ForwardInsecure: forwardInsecure == "on",
	})
	m.mu.Unlock()

	go func() {
		logger.Infof(
			context.Background(),
			"proxy [%s://%s:%s -> %s://%s:%s] is running",
			listenScheme, listenHost, listenPort, forwardScheme, forwardHost, forwardPort,
		)
		var err error
		if listenScheme == "https" {
			err = server.ListenAndServeTLS("", "")
		} else {
			err = server.ListenAndServe()
		}
		if err != nil {
			logger.Errorf(
				context.Background(),
				"proxy [%s://%s:%s -> %s://%s:%s] is stopped: %s",
				listenScheme, listenHost, listenPort, forwardScheme, forwardHost, forwardPort, err,
			)
		}

		m.mu.Lock()
		m.servers[&server] = Stopped
		if err != nil {
			m.lastError = err
		}
		m.mu.Unlock()
	}()
	return nil
}
