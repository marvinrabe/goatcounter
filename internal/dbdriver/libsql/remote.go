package libsql

import (
	"context"
	sqldriver "database/sql/driver"
	"time"
)

const remoteTimeout = 10 * time.Second

// The SDK honors query contexts, but its Tx.Commit/Rollback use Background.
// Keep those operations cancellable too, and bound calls without a deadline.
type remoteConnector struct{ sqldriver.Connector }

func (c *remoteConnector) Connect(ctx context.Context) (sqldriver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &remoteConn{Conn: conn}, nil
}

type remoteConn struct{ sqldriver.Conn }

func (c *remoteConn) Ping(ctx context.Context) error {
	_, err := c.ExecContext(ctx, "select 1", nil)
	return err
}

func (c *remoteConn) ExecContext(ctx context.Context, query string, args []sqldriver.NamedValue) (sqldriver.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	return c.Conn.(sqldriver.ExecerContext).ExecContext(ctx, query, args)
}

func (c *remoteConn) QueryContext(ctx context.Context, query string, args []sqldriver.NamedValue) (sqldriver.Rows, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	// The HTTP SDK materializes the response before returning Rows.
	return c.Conn.(sqldriver.QueryerContext).QueryContext(ctx, query, args)
}

func (c *remoteConn) Prepare(query string) (sqldriver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *remoteConn) PrepareContext(ctx context.Context, query string) (sqldriver.Stmt, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	stmt, err := c.Conn.(sqldriver.ConnPrepareContext).PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return &remoteStmt{Stmt: stmt}, nil
}

func (c *remoteConn) Begin() (sqldriver.Tx, error) {
	return c.BeginTx(context.Background(), sqldriver.TxOptions{})
}

func (c *remoteConn) BeginTx(ctx context.Context, opts sqldriver.TxOptions) (sqldriver.Tx, error) {
	callCtx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	if _, err := c.Conn.(sqldriver.ConnBeginTx).BeginTx(callCtx, opts); err != nil {
		return nil, err
	}
	return &remoteTx{conn: c, ctx: ctx}, nil
}

type remoteTx struct {
	conn *remoteConn
	ctx  context.Context
}

func (t *remoteTx) Commit() error {
	_, err := t.conn.ExecContext(t.ctx, "COMMIT", nil)
	return err
}

func (t *remoteTx) Rollback() error {
	// Cancellation of the transaction must still allow bounded cleanup.
	_, err := t.conn.ExecContext(context.Background(), "ROLLBACK", nil)
	return err
}

type remoteStmt struct{ sqldriver.Stmt }

func namedValues(args []sqldriver.Value) []sqldriver.NamedValue {
	out := make([]sqldriver.NamedValue, len(args))
	for i, value := range args {
		out[i] = sqldriver.NamedValue{Ordinal: i + 1, Value: value}
	}
	return out
}

func (s *remoteStmt) Exec(args []sqldriver.Value) (sqldriver.Result, error) {
	return s.ExecContext(context.Background(), namedValues(args))
}

func (s *remoteStmt) Query(args []sqldriver.Value) (sqldriver.Rows, error) {
	return s.QueryContext(context.Background(), namedValues(args))
}

func (s *remoteStmt) ExecContext(ctx context.Context, args []sqldriver.NamedValue) (sqldriver.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	return s.Stmt.(sqldriver.StmtExecContext).ExecContext(ctx, args)
}

func (s *remoteStmt) QueryContext(ctx context.Context, args []sqldriver.NamedValue) (sqldriver.Rows, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	return s.Stmt.(sqldriver.StmtQueryContext).QueryContext(ctx, args)
}
