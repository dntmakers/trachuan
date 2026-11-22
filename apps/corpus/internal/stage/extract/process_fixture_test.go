package extract

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func processFixtureDB(t *testing.T, fetch artifact.Result) *pgxpool.Pool {
	return processFixtureDBWithRow(t, fetch, nil)
}

func processFixtureDBWithRow(t *testing.T, fetch artifact.Result, row *store.GetRunOCRSelectionRow) *pgxpool.Pool {
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
			if serveErr := serveProcessFixture(server, fetch, row); serveErr != nil && !errors.Is(serveErr, io.EOF) && !errors.Is(serveErr, net.ErrClosed) {
				t.Errorf("process query fixture: %v", serveErr)
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

func serveProcessFixture(conn net.Conn, fetch artifact.Result, row *store.GetRunOCRSelectionRow) error {
	backend := pgproto3.NewBackend(conn, conn)
	if _, err := backend.ReceiveStartupMessage(); err != nil {
		return err
	}
	backend.Send(&pgproto3.AuthenticationOk{})
	for key, value := range map[string]string{"server_version": "18.0", "client_encoding": "UTF8", "standard_conforming_strings": "on"} {
		backend.Send(&pgproto3.ParameterStatus{Name: key, Value: value})
	}
	backend.Send(&pgproto3.BackendKeyData{ProcessID: 1, SecretKey: []byte{0, 0, 0, 1}})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := backend.Flush(); err != nil {
		return err
	}
	for {
		message, err := backend.Receive()
		if err != nil {
			return err
		}
		switch message := message.(type) {
		case *pgproto3.Terminate:
			return nil
		case *pgproto3.Query:
			switch {
			case strings.HasPrefix(message.String, "-- name: GetExtractInput :one"):
				backend.Send(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{
					{Name: []byte("path"), DataTypeOID: pgtype.TextOID, DataTypeSize: -1, TypeModifier: -1},
					{Name: []byte("sha256"), DataTypeOID: pgtype.TextOID, DataTypeSize: -1, TypeModifier: -1},
				}})
				backend.Send(&pgproto3.DataRow{Values: [][]byte{[]byte(fetch.Path), []byte(fetch.SHA256)}})
				backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
			case strings.HasPrefix(message.String, "-- name: ListExtractInputs :many"):
				backend.Send(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{
					{Name: []byte("item_key"), DataTypeOID: pgtype.TextOID, DataTypeSize: -1, TypeModifier: -1},
					{Name: []byte("sha256"), DataTypeOID: pgtype.TextOID, DataTypeSize: -1, TypeModifier: -1},
				}})
				backend.Send(&pgproto3.DataRow{Values: [][]byte{[]byte("vn:vbpl:1"), []byte(fetch.SHA256)}})
				backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
			case strings.HasPrefix(message.String, "-- name: GetRunOCRSelection :one"):
				if row == nil {
					backend.Send(&pgproto3.RowDescription{})
					backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 0")})
				} else {
					sendFixtureSelection(backend, *row)
				}
			default:
				return fmt.Errorf("unexpected query")
			}
			backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
			if err = backend.Flush(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected protocol message %T", message)
		}
	}
}

func sendFixtureSelection(backend *pgproto3.Backend, row store.GetRunOCRSelectionRow) {
	value := func(v any) []byte {
		switch x := v.(type) {
		case *string:
			if x == nil {
				return nil
			}
			return []byte(*x)
		case *int64:
			if x == nil {
				return nil
			}
			return []byte(fmt.Sprint(*x))
		case pgtype.Int4:
			if !x.Valid {
				return nil
			}
			return []byte(fmt.Sprint(x.Int32))
		case []byte:
			return x
		default:
			return []byte(fmt.Sprint(v))
		}
	}
	values := []any{row.RunID, row.ItemKey, row.Gap, row.PdfSha256, row.OcrTextID, row.OcrCheckID, row.SelectionVersion, row.SelectionState, row.TextSource, row.FetchSha256, row.TextPdfSha256, row.CheckOcrTextID, row.EngineID, row.TextSha256, row.CheckerID, row.ResultSha256, row.Flagged, row.CheckResult}
	oids := []uint32{pgtype.Int8OID, pgtype.TextOID, pgtype.TextOID, pgtype.TextOID, pgtype.Int8OID, pgtype.Int8OID, pgtype.Int4OID, pgtype.TextOID, pgtype.TextOID, pgtype.TextOID, pgtype.TextOID, pgtype.Int8OID, pgtype.TextOID, pgtype.TextOID, pgtype.TextOID, pgtype.TextOID, pgtype.Int4OID, pgtype.JSONBOID}
	fields := make([]pgproto3.FieldDescription, len(values))
	data := make([][]byte, len(values))
	for i, v := range values {
		fields[i] = pgproto3.FieldDescription{Name: []byte(fmt.Sprintf("col%d", i)), DataTypeOID: oids[i], DataTypeSize: -1, TypeModifier: -1}
		data[i] = value(v)
	}
	backend.Send(&pgproto3.RowDescription{Fields: fields})
	backend.Send(&pgproto3.DataRow{Values: data})
	backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
}
