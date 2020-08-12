package main

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
	luaparse "github.com/yuin/gopher-lua/parse"

	sqlgen "github.com/pingcap/go-randgen/grammar/sql_generator"
)

//go:generate go run modernc.org/assets -d lib/ -o lib.generated.go --map luaLibs

type genTestOptions struct {
	Grammar    string
	InitRoot   string
	TxnRoot    string
	RecurLimit int
	NumTxn     int
	Debug      bool
}

func genTest(opts genTestOptions) (test Test, err error) {
	rand.Seed(time.Now().UnixNano())

	it, err := sqlgen.NewSQLGen(opts.Grammar, nil, setup)
	if err != nil {
		return Test{}, err
	}
	it.SetRecurLimit(opts.RecurLimit).SetDebug(opts.Debug)

	it.SetRoot(opts.InitRoot)
	err = it.Visit(func(sql string) bool {
		test.InitSQL = append(test.InitSQL, sql)
		return it.PathInfo().Depth != 0
	})
	if err != nil {
		return Test{}, err
	}

	it.SetRoot(opts.TxnRoot)
	for i := 0; i < opts.NumTxn; i++ {
		txn := make(Txn, 0, 8)
		err = it.Visit(func(sql string) bool {
			txn = append(txn, Stmt{Stmt: sql})
			return it.PathInfo().Depth != 0
		})
		if err != nil {
			return Test{}, err
		}
		test.Steps = append(test.Steps, txn)
	}
	return
}

func setup(L *lua.LState, out io.Writer) error {
	L.SetGlobal("print", L.NewFunction(func(L *lua.LState) int {
		top := L.GetTop()
		for i := 1; i <= top; i++ {
			fmt.Fprint(out, L.ToStringMeta(L.Get(i)).String())
			if i != top {
				fmt.Fprint(out, "\t")
			}
		}
		return 0
	}))
	L.SetGlobal("printf", L.NewFunction(func(L *lua.LState) int {
		format := L.CheckString(1)
		args := make([]interface{}, L.GetTop()-1)
		top := L.GetTop()
		for i := 2; i <= top; i++ {
			args[i-2] = L.Get(i)
		}
		k := strings.Count(format, "%") - strings.Count(format, "%%")
		if len(args) < k {
			k = len(args)
		}
		fmt.Fprintf(out, format, args[:k]...)
		return 0
	}))
	L.SetGlobal("timef", L.NewFunction(func(L *lua.LState) int {
		t := time.Now()
		if L.GetTop() > 0 {
			t = time.Unix(L.CheckInt64(1), 0)
		}
		format := "2006-01-02 15:04:05"
		if L.GetTop() > 1 {
			format = L.CheckString(2)
		}
		L.Push(lua.LString(t.UTC().Format(format)))
		return 1
	}))
	L.SetGlobal("random_name", L.NewFunction(func(L *lua.LState) int {
		n := adjectives[rand.Intn(len(adjectives))] + " " + surnames[rand.Intn(len(surnames))]
		L.Push(lua.LString(n))
		return 1
	}))
	return preloadLib(L, "util")
}

func preloadLib(L *lua.LState, name string) error {
	src, ok := luaLibs["/"+name+".lua"]
	if !ok {
		return errors.New("lib not found: " + name)
	}
	preload := L.GetField(L.GetField(L.Get(lua.EnvironIndex), "package"), "preload")
	if _, ok := preload.(*lua.LTable); !ok {
		return errors.New("package.preload must be a table")
	}
	chunk, err := luaparse.Parse(strings.NewReader(src), name)
	if err != nil {
		return err
	}
	proto, err := lua.Compile(chunk, name)
	if err != nil {
		return err
	}
	L.SetField(preload, name, L.NewFunctionFromProto(proto))
	return nil
}
