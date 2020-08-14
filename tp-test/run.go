package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/zyguan/sqlz/resultset"

	. "github.com/zyguan/just"
)

type runABTestOptions struct {
	Threads      int
	QueryTimeout int
	Continue     func() bool

	Tag1 string
	Tag2 string
	DB1  *sql.DB
	DB2  *sql.DB

	Store Store
}

func runABTest(ctx context.Context, failed chan struct{}, opts runABTestOptions) error {

	store, db1, db2 := opts.Store, opts.DB1, opts.DB2
	if opts.Continue == nil {
		opts.Continue = func() bool { return true }
	}
	for opts.Continue() {
		select {
		case <-failed:
			return nil
		case <-ctx.Done():
			return nil
		default:
		}

		t, err := store.NextPendingTest()
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				log.Printf("no more test")
				select {
				case <-failed:
					return nil
				case <-ctx.Done():
					return nil
				case <-time.After(time.Duration(rand.Intn(10*opts.Threads)) * time.Second):
					continue
				}
			}
			return err
		}

		// using different db names allows us run test on same db instance
		dbName1 := "db1__" + strings.ReplaceAll(t.ID, "-", "_")
		dbName2 := "db2__" + strings.ReplaceAll(t.ID, "-", "_")

		if err1 := initTest(ctx, db1, dbName1, t); err1 != nil {
			store.SetTest(t.ID, TestFailed, "init db1: "+err1.Error())
			log.Printf("failed to init %s/%s: %v", opts.Tag1, dbName1, err1)
			continue
		}
		if err2 := initTest(ctx, db2, dbName2, t); err2 != nil {
			store.SetTest(t.ID, TestFailed, "init db2: "+err2.Error())
			log.Printf("failed to init %s/%s: %v", opts.Tag2, dbName2, err2)
			continue
		}

		log.Printf("run test %s", t.ID)

		// before all txn begin

		// begin txns
		txnOpts := sql.TxOptions{
			Isolation: sql.LevelReadCommitted,
			ReadOnly:  false,
		}
		db1Txns, _ := beginTxns(ctx, db1, dbName1, t, opts, &txnOpts)
		db2Txns, _ := beginTxns(ctx, db2, dbName2, t, opts, &txnOpts)

		fail := func(err error) error {
			defer func() { recover() }()
			for i := range db1Txns {
				db1Txns[i].Rollback()
				db2Txns[i].Rollback()
			}
			store.SetTest(t.ID, TestFailed, err.Error())
			log.Printf("test(%s) failed at: %v", t.ID, err)
			close(failed)
			return err
		}

		// The first statement of each txn must be a select; so that MySQL and TiDB both have Consistent Snapshot behavior.
		TxnOrder := make([]int, 0)
		TxnStmtCnt := make([]int, 0)
		stmts := make([]Stmt, 0)
		for txnID := range db1Txns {
			if err := executeStmt(ctx, opts, t, db1Txns[txnID], db2Txns[txnID], t.Steps[txnID][0]); err != nil {
				return fail(err)
			}
			for j := 1; j < len(t.Steps[txnID]); j++ {
				TxnOrder = append(TxnOrder, txnID)
			}
			TxnStmtCnt = append(TxnStmtCnt, 1)
		}

		rand.Seed(time.Now().UnixNano())
		rand.Shuffle(len(TxnOrder), func(i, j int) { TxnOrder[i], TxnOrder[j] = TxnOrder[j], TxnOrder[i] })
		// TODO txn order => stmt order
		for _, TxnID := range TxnOrder {
			stmts = append(stmts, t.Steps[TxnID][TxnStmtCnt[TxnID]])
			TxnStmtCnt[TxnID]++
		}

		log.Printf("[Debug]=============================shuffled stmts=========================:\n %v", stmts)

		log.Printf("start executing")
		// execute all stmts
		for _, stmt := range stmts {
			log.Printf("execute: %v", stmt)
			if err := executeStmt(ctx, opts, t, db1Txns[stmt.Txn], db2Txns[stmt.Txn], stmt); err != nil {
				return fail(err)
			}
		}
		log.Printf("start committing")
		// commit all txns
		for i := range db1Txns {
			err1, err2 := db1Txns[i].Commit(), db2Txns[i].Commit()
			if !validateErrs(err1, err2) {
				return fail(fmt.Errorf("commit txn #%d: %v <> %v", i, err1, err2))
			}
		}

		// check tables
		hs1, err1 := checkTables(ctx, db1, dbName1)
		if err1 != nil {
			return fail(fmt.Errorf("check table of %s: %v", opts.Tag1, err1))
		}
		hs2, err2 := checkTables(ctx, db2, dbName2)
		if err2 != nil {
			return fail(fmt.Errorf("check table of %s: %v", opts.Tag2, err2))
		}
		for t := range hs2 {
			if hs1[t] != hs2[t] {
				return fail(fmt.Errorf("data mismatch: %s != %s @%s", hs1[t], hs2[t], t))
			}
		}

		store.SetTest(t.ID, TestPassed, "")

		db1.ExecContext(ctx, "drop database if exists "+dbName1)
		db2.ExecContext(ctx, "drop database if exists "+dbName2)
	}
	return nil
}

func executeStmt(ctx context.Context, opts runABTestOptions, t *Test, tx1 *sql.Tx, tx2 *sql.Tx, stmt Stmt) error {
	record := func(seq int, tag string, rs *resultset.ResultSet, err error) {
		if rs != nil {
			raw, _ := rs.Encode()
			opts.Store.PutStmtResult(t.ID, seq, tag, Result{
				Raw:          raw,
				Err:          err,
				RowsAffected: rs.ExecResult().RowsAffected,
				LastInsertId: rs.ExecResult().LastInsertId,
			})
		} else {
			opts.Store.PutStmtResult(t.ID, seq, tag, Result{Err: err})
		}
	}

	ctx1, _ := context.WithTimeout(ctx, time.Duration(opts.QueryTimeout)*time.Second)
	rs1, err1 := doStmt(ctx1, tx1, stmt)
	record(stmt.Seq, opts.Tag1, rs1, err1)
	ctx2, _ := context.WithTimeout(ctx, time.Duration(opts.QueryTimeout)*time.Second)
	rs2, err2 := doStmt(ctx2, tx2, stmt)
	record(stmt.Seq, opts.Tag2, rs2, err2)
	if !validateErrs(err1, err2) {
		return fmt.Errorf("errors mismatch: %v <> %v @(%s,%d) %q", err1, err2, t.ID, stmt.Seq, stmt.Stmt)
	}
	if rs1 == nil || rs2 == nil {
		log.Printf("skip query error: [%v] [%v] @(%s,%d)", err1, err2, t.ID, stmt.Seq)
		return nil
	}
	h1, h2 := "", ""
	if q := strings.ToLower(stmt.Stmt); stmt.IsQuery && rs1.NRows() == rs2.NRows() && rs1.NRows() > 1 &&
		(!strings.Contains(q, "order by") || strings.Contains(q, "force-unordered")) {
		h1, h2 = unorderedDigest(rs1, nil), unorderedDigest(rs2, nil)
	} else {
		h1, h2 = rs1.DataDigest(), rs2.DataDigest()
	}
	if h1 != h2 {
		return fmt.Errorf("result digests mismatch: %s != %s @(%s,%d) %q", h1, h2, t.ID, stmt.Seq, stmt.Stmt)
	}
	if rs1.IsExecResult() && rs1.ExecResult().RowsAffected != rs2.ExecResult().RowsAffected {
		return fmt.Errorf("rows affected mismatch: %d != %d @(%s,%d) %q",
			rs1.ExecResult().RowsAffected, rs2.ExecResult().RowsAffected, t.ID, stmt.Seq, stmt.Stmt)
	}
	return nil
}

func beginTxns(ctx context.Context, db *sql.DB, dbName string, t *Test, opts runABTestOptions, txnOpts *sql.TxOptions) ([]*sql.Tx, error) {
	dbTxns := make([]*sql.Tx, 0)
	for i := range t.Steps {

		newTx, err := db.BeginTx(ctx, txnOpts)
		if err != nil {
			return nil, fmt.Errorf("start txn #%d of test(%s) on %s: %v", i, t.ID, opts.Tag1, err)
		}
		newTx.Exec("use " + dbName)
		dbTxns = append(dbTxns, newTx)
	}
	return dbTxns, nil
}

func initTest(ctx context.Context, db *sql.DB, name string, t *Test) (err error) {
	defer Return(&err)
	Try(db.ExecContext(ctx, "create database "+name))
	conn := Try(db.Conn(ctx)).(*sql.Conn)
	defer conn.Close()
	Try(conn.ExecContext(ctx, "use "+name))
	for _, stmt := range t.InitSQL {
		// it's ok for some of stmts in init_sql failed
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			log.Printf("init stmt failed: %v @ %s", err, stmt)
		}
	}
	return
}

func checkTables(ctx context.Context, db *sql.DB, name string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, "select table_name from information_schema.tables where table_schema = ?", name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hs := make(map[string]string)
	for rows.Next() {
		var t string
		if err = rows.Scan(&t); err != nil {
			return nil, err
		}
		if hs[t], err = checkTable(ctx, db, fmt.Sprintf("`%s`.`%s`", name, t)); err != nil {
			return nil, err
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return hs, nil
}

// FIXME show tables and check all
func checkTable(ctx context.Context, db *sql.DB, name string) (string, error) {
	_, err := db.ExecContext(ctx, "admin check table "+name)
	if err != nil {
		if e, ok := err.(*mysql.MySQLError); !ok || e.Number != 1064 {
			return "", err
		}
	}
	rows, err := db.QueryContext(ctx, "select * from "+name)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	rs, err := resultset.ReadFromRows(rows)
	if err != nil {
		return "", err
	}
	return unorderedDigest(rs, func(col resultset.ColumnDef) bool {
		return col.Type != "JSON"
	}), nil
}

type rows [][]byte

func (r rows) Len() int { return len(r) }

func (r rows) Less(i, j int) bool { return bytes.Compare(r[i], r[j]) < 0 }

func (r rows) Swap(i, j int) { r[i], r[j] = r[j], r[i] }

func unorderedDigest(rs *resultset.ResultSet, colFilter func(resultset.ColumnDef) bool) string {
	if colFilter == nil {
		colFilter = func(_ resultset.ColumnDef) bool { return true }
	}
	cols := make([]int, 0, rs.NCols())
	for i := 0; i < rs.NCols(); i++ {
		if colFilter(rs.ColumnDef(i)) {
			cols = append(cols, i)
		}
	}
	digests := make(rows, rs.NRows())
	for i := 0; i < rs.NRows(); i++ {
		h := sha1.New()
		for _, j := range cols {
			raw, _ := rs.RawValue(i, j)
			h.Write(raw)
		}
		digests[i] = h.Sum(nil)
	}
	sort.Sort(digests)
	h := sha1.New()
	for _, digest := range digests {
		h.Write(digest)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func doStmt(ctx context.Context, tx *sql.Tx, stmt Stmt) (*resultset.ResultSet, error) {
	if stmt.IsQuery {
		rows, err := tx.QueryContext(ctx, stmt.Stmt)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return resultset.ReadFromRows(rows)
	} else {
		res, err := tx.ExecContext(ctx, stmt.Stmt)
		if err != nil {
			return nil, err
		}
		return resultset.NewFromResult(res), nil
	}
}

func validateErrs(err1 error, err2 error) bool {
	return (err1 == nil && err2 == nil) || (err1 != nil && err2 != nil)
}
