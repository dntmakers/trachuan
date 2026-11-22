package stage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type reuseBatchProbe struct {
	Stage
	recipe              string
	verified, processed int
}

func (s *reuseBatchProbe) Recipe(Snapshot) string { return s.recipe }
func (s *reuseBatchProbe) VerifyReuse(context.Context, Env, Run, Item, artifact.Result) error {
	s.verified++
	return ErrSuperseded
}
func (s *reuseBatchProbe) Process(context.Context, Env, Run, Item) (Output, error) {
	s.processed++
	return Output{}, errors.New("unexpected fresh processing")
}

func reuseBatchDB(t *testing.T, input, recipe string) *pgxpool.Pool {
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
			if serveErr := serveReuseBatch(server, input, recipe); serveErr != nil && !errors.Is(serveErr, io.EOF) && !errors.Is(serveErr, net.ErrClosed) {
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

func serveReuseBatch(conn net.Conn, input, recipe string) error {
	backend := pgproto3.NewBackend(conn, conn)
	if _, err := backend.ReceiveStartupMessage(); err != nil {
		return err
	}
	backend.Send(&pgproto3.AuthenticationOk{})
	for key, value := range map[string]string{"server_version": "18.0", "client_encoding": "UTF8", "standard_conforming_strings": "on"} {
		backend.Send(&pgproto3.ParameterStatus{Name: key, Value: value})
	}
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := backend.Flush(); err != nil {
		return err
	}
	snapshot, err := json.Marshal(Snapshot{Recipes: map[string]string{"extract": recipe}})
	if err != nil {
		return err
	}
	stamp := "2024-01-01 00:00:00+00"
	for {
		message, receiveErr := backend.Receive()
		if receiveErr != nil {
			return receiveErr
		}
		switch msg := message.(type) {
		case *pgproto3.Terminate:
			return nil
		case *pgproto3.Query:
			var values [][]byte
			var oids []uint32
			switch {
			case strings.HasPrefix(msg.String, "-- name: GetBatchRun :one"):
				values = [][]byte{[]byte("1"), []byte("vn"), []byte("20990101"), []byte("1"), []byte("running"), nil, nil, nil, snapshot, nil, []byte(stamp), []byte(stamp)}
				oids = []uint32{pgtype.Int8OID, pgtype.TextOID, pgtype.TextOID, pgtype.Int4OID, pgtype.TextOID, pgtype.TextOID, pgtype.TextOID, pgtype.TextOID, pgtype.JSONBOID, pgtype.TextOID, pgtype.TimestamptzOID, pgtype.TimestamptzOID}
			case strings.HasPrefix(msg.String, "-- name: ListPendingBatchItems :many"):
				values = [][]byte{[]byte("1"), []byte("extract"), []byte("vn:vbpl:1"), []byte(input), []byte(recipe), []byte("pending"), []byte("0"), []byte("1"), []byte("0"), nil, nil, []byte(stamp)}
				oids = []uint32{pgtype.Int8OID, pgtype.TextOID, pgtype.TextOID, pgtype.TextOID, pgtype.TextOID, pgtype.TextOID, pgtype.Int4OID, pgtype.Int4OID, pgtype.Int4OID, pgtype.Int8OID, pgtype.TextOID, pgtype.TimestamptzOID}
			case strings.HasPrefix(msg.String, "-- name: GetBatchRunState :one"):
				values = [][]byte{[]byte("running")}
				oids = []uint32{pgtype.TextOID}
			default:
				return fmt.Errorf("unexpected batch query")
			}
			fields := make([]pgproto3.FieldDescription, len(values))
			for i, oid := range oids {
				fields[i] = pgproto3.FieldDescription{Name: []byte(fmt.Sprintf("col%d", i)), DataTypeOID: oid, DataTypeSize: -1, TypeModifier: -1}
			}
			backend.Send(&pgproto3.RowDescription{Fields: fields})
			backend.Send(&pgproto3.DataRow{Values: values})
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

func TestRunBatchChecksCachedArtifactAuthority(t *testing.T) {
	ctx := context.Background()
	store := artifact.Store{Root: t.TempDir()}
	input, recipe := strings.Repeat("a", 64), strings.Repeat("b", 64)
	if _, err := store.Publish(ctx, "extract", input, recipe, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	db := reuseBatchDB(t, input, recipe)
	probe := &reuseBatchProbe{recipe: recipe}
	_, err := RunBatch(ctx, Env{DB: db, Artifacts: store}, probe, BatchInput{RunID: 1, Generation: 1, Stage: "extract", Batch: 0}, 1, BatchConfig{HeartbeatEvery: time.Hour, MaxAttempts: 1}, func(...any) {})
	if !errors.Is(err, ErrSuperseded) || probe.verified != 1 || probe.processed != 0 {
		t.Fatalf("err=%v verified=%d processed=%d", err, probe.verified, probe.processed)
	}
}
