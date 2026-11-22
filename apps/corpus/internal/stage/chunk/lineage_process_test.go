package chunk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func chunkFixtureDB(t *testing.T, fetched, extracted artifact.Result, recipe string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("host=127.0.0.1 user=fixture dbname=fixture sslmode=disable pool_max_conns=1")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	var workers sync.WaitGroup
	cfg.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { _ = server.Close() }()
			if serveErr := serveChunkFixture(server, fetched, extracted, recipe); serveErr != nil && !errors.Is(serveErr, io.EOF) && !errors.Is(serveErr, net.ErrClosed) {
				t.Errorf("fixture: %v", serveErr)
			}
		}()
		return client, nil
	}
	db, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); workers.Wait() })
	return db
}

func serveChunkFixture(conn net.Conn, fetched, extracted artifact.Result, recipe string) error {
	backend := pgproto3.NewBackend(conn, conn)
	if _, err := backend.ReceiveStartupMessage(); err != nil {
		return err
	}
	backend.Send(&pgproto3.AuthenticationOk{})
	backend.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "18.0"})
	backend.Send(&pgproto3.ParameterStatus{Name: "standard_conforming_strings", Value: "on"})
	backend.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := backend.Flush(); err != nil {
		return err
	}
	for {
		message, err := backend.Receive()
		if err != nil {
			return err
		}
		switch query := message.(type) {
		case *pgproto3.Terminate:
			return nil
		case *pgproto3.Query:
			if !strings.HasPrefix(query.String, "-- name: GetChunkInput :one") {
				return fmt.Errorf("unexpected query")
			}
			values := []string{fetched.Path, fetched.SHA256, extracted.Path, extracted.SHA256, extracted.InputHash, recipe, "vn:vbpl:1", "vn:vbpl:1"}
			fields := make([]pgproto3.FieldDescription, len(values))
			data := make([][]byte, len(values))
			for i, value := range values {
				fields[i] = pgproto3.FieldDescription{Name: []byte(fmt.Sprintf("col%d", i)), DataTypeOID: pgtype.TextOID, DataTypeSize: -1, TypeModifier: -1}
				data[i] = []byte(value)
			}
			backend.Send(&pgproto3.RowDescription{Fields: fields})
			backend.Send(&pgproto3.DataRow{Values: data})
			backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
			backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
			if err = backend.Flush(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected protocol message %T", message)
		}
	}
}

func TestChunkProcessUsesVerifiedRecipeContract(t *testing.T) {
	ctx := context.Background()
	artifacts := artifact.Store{Root: t.TempDir()}
	fetched, err := artifacts.PublishContent(ctx, "fetch", strings.Repeat("a", 64), func(dir string) error {
		if writeErr := os.WriteFile(filepath.Join(dir, "detail.json"), []byte(`{"data":{"id":"1"}}`), 0600); writeErr != nil {
			return writeErr
		}
		return os.WriteFile(filepath.Join(dir, "files.json"), []byte(`[]`), 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	recipe := strings.Repeat("b", 64)
	for _, tc := range []struct {
		name, contract, declared string
		text, ok                 bool
	}{
		{"legacy", stage.ExtractRecordLegacy, recipe, false, true},
		{"v1 missing", stage.ExtractRecordV1, recipe, false, false},
		{"v1 present", stage.ExtractRecordV1, recipe, true, true},
		{"missing metadata", "", "", false, false},
		{"recipe mismatch", stage.ExtractRecordLegacy, strings.Repeat("c", 64), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := artifact.Store{Root: t.TempDir()}
			extracted, e := root.Publish(ctx, "extract", fetched.SHA256, recipe, func(dir string) error {
				if writeErr := os.WriteFile(filepath.Join(dir, "provisions.json"), []byte(`[]`), 0600); writeErr != nil {
					return writeErr
				}
				if tc.text {
					return os.WriteFile(filepath.Join(dir, "text.json"), []byte(`{"text_source":"source_tree","detector_inputs":{"version":1,"ocr_check":{"state":"not_selected"},"tree_transcription":{"state":"no_pair"}}}`), 0600)
				}
				return nil
			})
			if e != nil {
				t.Fatal(e)
			}
			fetchedAbsolute, extractedAbsolute := fetched, extracted
			fetchedAbsolute.Path, extractedAbsolute.Path = artifacts.Abs(fetched.Path), root.Abs(extracted.Path)
			db := chunkFixtureDB(t, fetchedAbsolute, extractedAbsolute, recipe)
			run := stage.Run{ID: 1, Snapshot: stage.Snapshot{ExtractRecordContract: tc.contract, Recipes: map[string]string{"extract": tc.declared}}}
			out, processErr := New().Process(ctx, stage.Env{DB: db, Artifacts: root}, run, stage.Item{Key: "vn:vbpl:1", InputHash: inputHash("vn:vbpl:1", fetched.SHA256, extracted.SHA256)})
			if (processErr == nil) != tc.ok || (tc.ok && out.Artifact == nil) || (!tc.ok && out.Artifact != nil) {
				t.Fatalf("out=%+v err=%v", out, processErr)
			}
		})
	}
}
