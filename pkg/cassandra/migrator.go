package cassandra

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Executor interface {
	Exec(ctx context.Context, statement string, values ...interface{}) error
	Versions(ctx context.Context, keyspace string) ([]int, error)
}

type Migrator struct {
	exec     Executor
	keyspace string
	files    fs.FS
}

func NewMigrator(exec Executor, keyspace string, files fs.FS) *Migrator {
	return &Migrator{exec: exec, keyspace: keyspace, files: files}
}

func (m *Migrator) Up(ctx context.Context) ([]int, error) {
	names, err := fs.Glob(m.files, "*.cql")
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no migration files found")
	}
	sort.Strings(names)

	if err := m.apply(ctx, names[0]); err != nil {
		return nil, err
	}
	if err := m.exec.Exec(ctx, fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.schema_migrations (version int PRIMARY KEY, applied_at timestamp)", m.keyspace)); err != nil {
		return nil, err
	}

	versions, err := m.exec.Versions(ctx, m.keyspace)
	if err != nil {
		return nil, err
	}
	applied := map[int]bool{}
	for _, version := range versions {
		applied[version] = true
	}

	var newlyApplied []int
	for _, name := range names {
		version, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return nil, fmt.Errorf("migration %s: %w", name, err)
		}
		if applied[version] {
			continue
		}
		if err := m.apply(ctx, name); err != nil {
			return nil, fmt.Errorf("migration %s: %w", name, err)
		}
		if err := m.exec.Exec(ctx, fmt.Sprintf("INSERT INTO %s.schema_migrations (version, applied_at) VALUES (?, ?)", m.keyspace), version, time.Now().UTC()); err != nil {
			return nil, err
		}
		newlyApplied = append(newlyApplied, version)
	}
	return newlyApplied, nil
}

func (m *Migrator) apply(ctx context.Context, name string) error {
	content, err := fs.ReadFile(m.files, name)
	if err != nil {
		return err
	}
	for _, statement := range strings.Split(strings.ReplaceAll(string(content), "{{keyspace}}", m.keyspace), ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if err := m.exec.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
