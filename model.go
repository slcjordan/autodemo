package autodemo

import "time"

type History struct {
	Index    int
	Args     []string
	Output   string
	ExecTime time.Duration
}

type Project struct {
	Name       string
	WorkingDir string
	Desc       string
}
