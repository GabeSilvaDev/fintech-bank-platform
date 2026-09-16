package database

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type Migrator struct {
	session  *gocql.Session
	keyspace string
	files    fs.FS
}

func NewMigrator(session *gocql.Session, keyspace string, files fs.FS) *Migrator {
	return &Migrator{session: session, keyspace: keyspace, files: files}
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

	if err := m.exec(ctx, names[0]); err != nil {
		return nil, err
	}
	if err := m.session.Query(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.schema_migrations (version int PRIMARY KEY, applied_at timestamp)", m.keyspace)).WithContext(ctx).Exec(); err != nil {
		return nil, err
	}

	applied, err := m.applied(ctx)
	if err != nil {
		return nil, err
	}

	var newlyApplied []int
	for _, name := range names {
		version, err := strconv.Atoi(name[:3])
		if err != nil {
			return nil, fmt.Errorf("migration %s: %w", name, err)
		}
		if applied[version] {
			continue
		}
		if err := m.exec(ctx, name); err != nil {
			return nil, fmt.Errorf("migration %s: %w", name, err)
		}
		if err := m.session.Query(fmt.Sprintf("INSERT INTO %s.schema_migrations (version, applied_at) VALUES (?, ?)", m.keyspace), version, time.Now().UTC()).WithContext(ctx).Exec(); err != nil {
			return nil, err
		}
		newlyApplied = append(newlyApplied, version)
	}
	return newlyApplied, nil
}

func (m *Migrator) applied(ctx context.Context) (map[int]bool, error) {
	iter := m.session.Query(fmt.Sprintf("SELECT version FROM %s.schema_migrations", m.keyspace)).WithContext(ctx).Iter()
	applied := map[int]bool{}
	var version int
	for iter.Scan(&version) {
		applied[version] = true
	}
	return applied, iter.Close()
}

func (m *Migrator) exec(ctx context.Context, name string) error {
	content, err := fs.ReadFile(m.files, name)
	if err != nil {
		return err
	}
	for _, statement := range strings.Split(strings.ReplaceAll(string(content), "{{keyspace}}", m.keyspace), ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if err := m.session.Query(statement).WithContext(ctx).Exec(); err != nil {
			return err
		}
	}
	return nil
}
