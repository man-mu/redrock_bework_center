// Package db 负责打开 SQLite 与建表（表结构见 docs/site-data-model.md §6）。
package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，注册 database/sql
)

// Open 打开 SQLite 数据库并应用基础 pragma。
func Open(path string) (*sql.DB, error) {
	d, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1) // SQLite 单写者：串行化访问，避免 busy 冲突
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := d.Exec(pragma); err != nil {
			d.Close()
			return nil, fmt.Errorf("exec %s: %w", pragma, err)
		}
	}
	if path != ":memory:" {
		if _, err := d.Exec("PRAGMA journal_mode = WAL"); err != nil {
			d.Close()
			return nil, fmt.Errorf("set WAL: %w", err)
		}
	}
	return d, nil
}

// EnsureSchema 建表（幂等）。
// 注意：commit 是 SQLite 保留字，DDL 与查询一律用双引号包住列名。
func EnsureSchema(d *sql.DB) error {
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS lessons (
			id     INTEGER PRIMARY KEY,
			number INTEGER NOT NULL UNIQUE,
			slug   TEXT    NOT NULL UNIQUE,
			title  TEXT    NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS exercises (
			id        INTEGER PRIMARY KEY,
			lesson_id INTEGER NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
			slug      TEXT    NOT NULL,
			position  INTEGER NOT NULL,
			UNIQUE (lesson_id, slug)
		)`,
		`CREATE TABLE IF NOT EXISTS students (
			id         INTEGER PRIMARY KEY,
			repo       TEXT NOT NULL UNIQUE,
			owner      TEXT NOT NULL,
			repo_url   TEXT NOT NULL,
			name       TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_students_name ON students(name)`,
		`CREATE TABLE IF NOT EXISTS reports (
			id          INTEGER PRIMARY KEY,
			student_id  INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
			lesson_id   INTEGER NOT NULL REFERENCES lessons(id),
			"commit"    TEXT    NOT NULL,
			ref         TEXT    NOT NULL DEFAULT '',
			payload     TEXT    NOT NULL,
			received_at TEXT    NOT NULL,
			UNIQUE (student_id, "commit")
		)`,
		`CREATE TABLE IF NOT EXISTS student_lessons (
			student_id  INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
			lesson_id   INTEGER NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
			completed   INTEGER NOT NULL,
			"commit"    TEXT    NOT NULL,
			reported_at TEXT    NOT NULL,
			PRIMARY KEY (student_id, lesson_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sl_lesson ON student_lessons(lesson_id)`,
		`CREATE TABLE IF NOT EXISTS course_sync (
			id        INTEGER PRIMARY KEY CHECK (id = 1),
			repo      TEXT NOT NULL,
			ref       TEXT NOT NULL,
			synced_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range ddl {
		if _, err := d.Exec(stmt); err != nil {
			return fmt.Errorf("exec ddl: %w", err)
		}
	}
	return nil
}
