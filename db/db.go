package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	_ "embed"

	_ "github.com/mattn/go-sqlite3"

	"github.com/slcjordan/autodemo"
	"github.com/slcjordan/autodemo/db/sqlc"
	"github.com/slcjordan/autodemo/logger"
)

type Conn struct {
	db *sql.DB
	tx *sql.Tx
}

func Open(filename string) (*Conn, error) {
	// db, err := sql.Open("sqlite3", filename+"?_journal_mode=WAL")
	db, err := sql.Open("sqlite3", filename)
	if err != nil {
		return nil, err
	}
	return &Conn{db: db}, nil
}

func (c *Conn) Close() error {
	return c.db.Close()
}

func stringFrom(s string) sql.NullString {
	return sql.NullString{
		String: s,
		Valid:  true,
	}
}

func (c *Conn) ApplySchema(ctx context.Context) error {
	queries := sqlc.New(c.db)
	return queries.Schema(ctx)
}

func (c *Conn) MaybeSaveHistoryJob(ctx context.Context, project string, history autodemo.History) error {
	queries := sqlc.New(c.db)
	data, err := json.Marshal(history)
	if err != nil {
		return err
	}
	params := sqlc.MaybeCreateWorkParams{
		Data:    data,
		Project: project,
		Domain:  "history",
	}
	return queries.MaybeCreateWork(ctx, params)
}

func (c *Conn) DoNextHistoryJob(ctx context.Context, project autodemo.Project, f func(context.Context, autodemo.Project, autodemo.History) error) (resultErr error) {
	queries := sqlc.New(c.db)
	q := (queries).WithTx(c.tx)
	work, err := q.GetWork(
		ctx,
		sqlc.GetWorkParams{
			Domain:  "history",
			Project: project.Name,
			Status:  "done",
		})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return autodemo.IterDone
		}
		return err
	}
	data, err := q.FinishWork(ctx, work.ID)
	if err != nil {
		return err
	}
	var history autodemo.History
	err = json.Unmarshal(data, &history)
	if err != nil {
		return err
	}
	return f(ctx, project, history)
}

func (c *Conn) MaybeSaveProjectJob(ctx context.Context, project autodemo.Project) error {
	queries := sqlc.New(c.db)
	data, err := json.Marshal(project)
	if err != nil {
		return err
	}
	params := sqlc.MaybeCreateWorkParams{
		Data:   data,
		Domain: "project",
	}
	return queries.MaybeCreateWork(ctx, params)
}

func (c *Conn) DoNextProjectJob(ctx context.Context, f func(context.Context, string, autodemo.Project) error) (resultErr error) {
	queries := sqlc.New(c.db)
	work, err := queries.GetWork(
		ctx,
		sqlc.GetWorkParams{
			Domain: "project",
			Status: "done",
		})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return autodemo.IterDone
		}
		return err
	}

	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if resultErr == nil {
			resultErr = tx.Commit()
		}
		if resultErr != nil {
			tx.Rollback()
			err := queries.ErrorWork(ctx, sqlc.ErrorWorkParams{
				ID:    work.ID,
				Error: resultErr.Error(),
			})
			if err != nil {
				logger.Errorf(context.Background(), "could not save project error: %s", err)
			}
		}
	}()
	q := (queries).WithTx(tx)
	var data []byte
	switch work.Status {
	case "pending":
		data, err = q.PostprocessWork(ctx, work.ID)
		if err != nil {
			return err
		}
	case "postprocessing":
		data, err = q.FinishWork(ctx, work.ID)
		if err != nil {
			return err
		}
	}
	var project autodemo.Project
	err = json.Unmarshal(data, &project)
	if err != nil {
		return err
	}
	c.tx = tx
	err = f(ctx, work.Status, project)
	if err != nil {
		os.WriteFile(filepath.Join(project.WorkingDir, project.Name, "error.txt"), []byte(err.Error()), 0644)
		return err
	}
	return nil
}
