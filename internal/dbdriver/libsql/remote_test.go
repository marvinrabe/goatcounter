package libsql

import (
	"context"
	sqldriver "database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	remotelibsql "github.com/tursodatabase/libsql-client-go/libsql"
)

func TestRemoteCancellation(t *testing.T) {
	for _, operation := range []string{"exec", "query", "prepared", "commit"} {
		t.Run(operation, func(t *testing.T) {
			started := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Requests []struct{ Stmt struct{ SQL string } }
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				for _, item := range request.Requests {
					if item.Stmt.SQL == "select 42" || item.Stmt.SQL == "COMMIT" {
						started <- struct{}{}
						<-r.Context().Done()
						return
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"baton":"test","results":[{"type":"ok","response":{"type":"execute","result":{"cols":[],"rows":[],"affected_row_count":0,"last_insert_rowid":null}}}]}`))
			}))
			t.Cleanup(server.Close)
			t.Cleanup(server.CloseClientConnections)
			base, err := remotelibsql.NewConnector(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			conn, err := (&remoteConnector{Connector: base}).Connect(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			c := conn.(*remoteConn)
			result := make(chan error, 1)
			go func() {
				var err error
				switch operation {
				case "exec":
					_, err = c.ExecContext(ctx, "select 42", nil)
				case "query":
					_, err = c.QueryContext(ctx, "select 42", nil)
				case "prepared":
					var stmt sqldriver.Stmt
					stmt, err = c.PrepareContext(ctx, "select 42")
					if err == nil {
						defer stmt.Close()
						_, err = stmt.(sqldriver.StmtExecContext).ExecContext(ctx, nil)
					}
				case "commit":
					var tx sqldriver.Tx
					tx, err = c.BeginTx(ctx, sqldriver.TxOptions{})
					if err == nil {
						err = tx.Commit()
					}
				}
				result <- err
			}()
			select {
			case <-started:
			case err := <-result:
				t.Fatalf("operation did not reach server: %v", err)
			case <-time.After(2 * time.Second):
				t.Fatal("operation did not reach server")
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("want cancellation, got %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("database operation ignored cancellation")
			}
		})
	}
}
