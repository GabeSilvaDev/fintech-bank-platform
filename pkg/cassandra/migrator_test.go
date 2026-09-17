package cassandra

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
)

type fakeExecutor struct {
	statements []string
	versions   []int
	failOn     string
	versionErr error
}

func (f *fakeExecutor) Exec(_ context.Context, statement string, values ...interface{}) error {
	if f.failOn != "" && len(statement) >= len(f.failOn) && statement[:len(f.failOn)] == f.failOn {
		return errors.New("boom")
	}
	f.statements = append(f.statements, statement)
	if len(values) == 2 {
		f.versions = append(f.versions, values[0].(int))
	}
	return nil
}

func (f *fakeExecutor) Versions(context.Context, string) ([]int, error) {
	return f.versions, f.versionErr
}

func files() fstest.MapFS {
	return fstest.MapFS{
		"001_keyspace.cql": {Data: []byte("CREATE KEYSPACE IF NOT EXISTS {{keyspace}} WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};")},
		"002_a.cql":        {Data: []byte("CREATE TABLE IF NOT EXISTS {{keyspace}}.a (id int PRIMARY KEY);\nCREATE TABLE IF NOT EXISTS {{keyspace}}.b (id int PRIMARY KEY);")},
		"003_c.cql":        {Data: []byte("CREATE TABLE IF NOT EXISTS {{keyspace}}.c (id int PRIMARY KEY);")},
		"notes.txt":        {Data: []byte("ignored")},
	}
}

func TestMigratorAppliesEverythingOnce(t *testing.T) {
	exec := &fakeExecutor{}
	migrator := NewMigrator(exec, "ks", files())

	applied, err := migrator.Up(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, applied)
	assert.Equal(t, "CREATE KEYSPACE IF NOT EXISTS ks WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}", exec.statements[0])
	assert.Equal(t, "CREATE TABLE IF NOT EXISTS ks.schema_migrations (version int PRIMARY KEY, applied_at timestamp)", exec.statements[1])
	assert.Contains(t, exec.statements, "CREATE TABLE IF NOT EXISTS ks.b (id int PRIMARY KEY)")
	assert.Equal(t, []int{1, 2, 3}, exec.versions)

	again, err := migrator.Up(context.Background())
	assert.NoError(t, err)
	assert.Empty(t, again)
}

func TestMigratorSkipsAppliedVersions(t *testing.T) {
	exec := &fakeExecutor{versions: []int{1, 2}}

	applied, err := NewMigrator(exec, "ks", files()).Up(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, []int{3}, applied)
}

func TestMigratorErrors(t *testing.T) {
	_, err := NewMigrator(&fakeExecutor{}, "ks", fstest.MapFS{}).Up(context.Background())
	assert.EqualError(t, err, "no migration files found")

	_, err = NewMigrator(&fakeExecutor{}, "ks", fstest.MapFS{"abc_x.cql": {Data: []byte("SELECT 1;")}}).Up(context.Background())
	assert.ErrorContains(t, err, "migration abc_x.cql")

	_, err = NewMigrator(&fakeExecutor{failOn: "CREATE KEYSPACE"}, "ks", files()).Up(context.Background())
	assert.EqualError(t, err, "boom")

	_, err = NewMigrator(&fakeExecutor{failOn: "CREATE TABLE IF NOT EXISTS ks.schema_migrations"}, "ks", files()).Up(context.Background())
	assert.EqualError(t, err, "boom")

	_, err = NewMigrator(&fakeExecutor{versionErr: errors.New("versions down")}, "ks", files()).Up(context.Background())
	assert.EqualError(t, err, "versions down")

	_, err = NewMigrator(&fakeExecutor{failOn: "CREATE TABLE IF NOT EXISTS ks.c"}, "ks", files()).Up(context.Background())
	assert.ErrorContains(t, err, "migration 003_c.cql: boom")

	_, err = NewMigrator(&fakeExecutor{failOn: "INSERT INTO ks.schema_migrations"}, "ks", files()).Up(context.Background())
	assert.EqualError(t, err, "boom")
}

type globErrorFS struct{}

func (globErrorFS) Open(string) (fs.File, error) {
	return nil, fs.ErrNotExist
}

func (globErrorFS) Glob(string) ([]string, error) {
	return nil, errors.New("glob boom")
}

type readFileErrorFS struct{}

func (readFileErrorFS) Open(string) (fs.File, error) {
	return nil, fs.ErrNotExist
}

func (readFileErrorFS) Glob(string) ([]string, error) {
	return []string{"001_x.cql"}, nil
}

func (readFileErrorFS) ReadFile(string) ([]byte, error) {
	return nil, errors.New("read boom")
}

func TestMigratorGlobError(t *testing.T) {
	_, err := NewMigrator(&fakeExecutor{}, "ks", globErrorFS{}).Up(context.Background())
	assert.EqualError(t, err, "glob boom")
}

func TestMigratorReadFileError(t *testing.T) {
	_, err := NewMigrator(&fakeExecutor{}, "ks", readFileErrorFS{}).Up(context.Background())
	assert.EqualError(t, err, "read boom")
}
