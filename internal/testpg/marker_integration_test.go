//go:build integration

package testpg

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestDropIdleMarkedCorpus_LeavesConnectedDatabase(t *testing.T) {
	ctx := context.Background()
	admin, base := administrator(t)
	name, key, err := CreateCorpus(ctx, admin, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { DropCorpusDatabase(t, admin, name, key) })
	conn, err := pgx.Connect(ctx, databaseURL(base, name))
	if err != nil {
		t.Fatal(err)
	}
	dropped, err := dropIdleForTest(t, ctx, admin, name, key)
	if err != nil || dropped {
		t.Fatalf("connected database: dropped=%v error=%v", dropped, err)
	}
	second, err := pgx.Connect(ctx, databaseURL(base, name))
	if err != nil {
		t.Fatalf("reconnect after skip: %v", err)
	}
	if closeErr := second.Close(ctx); closeErr != nil {
		t.Fatal(closeErr)
	}
	if closeErr := conn.Close(ctx); closeErr != nil {
		t.Fatal(closeErr)
	}
	dropped, err = dropIdleForTest(t, ctx, admin, name, "vn/20990101")
	if err != nil || dropped {
		t.Fatalf("foreign marker: dropped=%v error=%v", dropped, err)
	}
	dropped, err = dropIdleForTest(t, ctx, admin, name, key)
	if err != nil || !dropped {
		t.Fatalf("idle marked database: dropped=%v error=%v", dropped, err)
	}
}

func TestDropIdleMarkedCorpus_BuilderOwned(t *testing.T) {
	ctx := context.Background()
	admin, _ := administrator(t)
	name, key, err := CreateCorpus(ctx, admin, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	role := ""
	roleCreated := false
	t.Cleanup(func() {
		DropCorpusDatabase(t, admin, name, key)
		if roleCreated {
			if _, execErr := admin.Exec(ctx, "DROP ROLE "+pgx.Identifier{role}.Sanitize()); execErr != nil {
				t.Errorf("drop role: %v", execErr)
			}
		}
	})
	role = "test_builder_" + random(t)
	if _, execErr := admin.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN CREATEDB"); execErr != nil {
		t.Fatal(execErr)
	}
	roleCreated = true
	if _, execErr := admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{name}.Sanitize()+" OWNER TO "+pgx.Identifier{role}.Sanitize()); execErr != nil {
		t.Fatal(execErr)
	}
	dropped, err := dropIdleForTest(t, ctx, admin, name, key)
	if err != nil || !dropped {
		t.Fatalf("builder-owned database: dropped=%v error=%v", dropped, err)
	}
}

func TestDropIdleMarkedCorpus_LeavesBuilderTemplate(t *testing.T) {
	ctx := context.Background()
	admin, _ := administrator(t)
	name, key, err := CreateCorpus(ctx, admin, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { DropCorpusDatabase(t, admin, name, key) })
	template := "test_template_" + random(t)
	if _, err = admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{name}.Sanitize()+" RENAME TO "+pgx.Identifier{template}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	name = template
	dropped, err := dropIdleForTest(t, ctx, admin, name, key)
	if err != nil || dropped {
		t.Fatalf("builder template: dropped=%v error=%v", dropped, err)
	}
}

func dropIdleForTest(t *testing.T, ctx context.Context, conn *pgx.Conn, name, runKey string) (bool, error) {
	t.Helper()
	var marker DatabaseMarker
	if err := conn.QueryRow(ctx, `SELECT oid, shobj_description(oid, 'pg_database') FROM pg_database WHERE datname=$1`, name).Scan(&marker.OID, &marker.Marker); err != nil {
		t.Fatal(err)
	}
	if MarkerRunKey(&marker.Marker) != runKey {
		marker.Marker = `{"run_key":"` + runKey + `"}`
	}
	dropped, _, err := DropIdleMarkedCorpus(ctx, conn, name, marker)
	return dropped, err
}

func TestDropIdleMarkedCorpus_RejectsChangedIdentity(t *testing.T) {
	ctx := context.Background()
	admin, _ := administrator(t)
	name, key, err := CreateCorpus(ctx, admin, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { DropCorpusDatabase(t, admin, name, key) })
	var snapshot DatabaseMarker
	if err := admin.QueryRow(ctx, `SELECT oid, shobj_description(oid, 'pg_database') FROM pg_database WHERE datname=$1`, name).Scan(&snapshot.OID, &snapshot.Marker); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []DatabaseMarker{
		{OID: snapshot.OID + 1, Marker: snapshot.Marker},
		{OID: snapshot.OID, Marker: snapshot.Marker + " "},
	} {
		dropped, _, err := DropIdleMarkedCorpus(ctx, admin, name, changed)
		if err != nil || dropped {
			t.Fatalf("changed identity: dropped=%v error=%v", dropped, err)
		}
	}
}
