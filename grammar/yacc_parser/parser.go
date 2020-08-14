package yacc_parser

import (
	"bytes"
	"errors"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
)

// token sequence of one branch
type Seq struct {
	Items []Token
	// production the Seq belong to
	PNumber int
	// number of the Seq in the production
	SNumber int
	// Attributes
	Weight float64
	// Transaction ID+1, used for conditional generation, TxnNum = 0 means any txn.
	// i.e. the branch will only be generated in txn (TxnNum-1)
	TxnNum int
}

func NewSeq(items []Token) (seq *Seq) {
	return &Seq{Weight: 1.0, Items: items}
}

func (s *Seq) Append(t Token) error {
	attr, ok := t.(*attribute)
	if !ok {
		s.Items = append(s.Items, t)
		return nil
	}
	raw := strings.SplitN(strings.Trim(attr.OriginString(), "[] "), "=", 2)
	switch strings.TrimSpace(raw[0]) {
	case "weight":
		if len(raw) != 2 {
			return errors.New("invalid attribute string: " + t.OriginString())
		}
		v, err := strconv.ParseFloat(raw[1], 64)
		if err != nil {
			return errors.New("invalid weight value: " + err.Error())
		}
		s.Weight = v
	case "ignore", "omit":
		s.Weight = 0
	case "txn":
		if len(raw) != 2 {
			return errors.New("invalid attribute string: " + t.OriginString())
		}
		v, err := strconv.ParseInt(raw[1], 10, 32)
		if err != nil {
			return errors.New("invalid txn number: " + err.Error())
		}
		s.TxnNum = int(v + 1)
		// log.Printf("TxnNum=%d, %v", int(v+1), s)
	default:
		return errors.New("unknown attribute string: " + t.OriginString())
	}
	return nil
}

func (s Seq) String() string {
	buf := &bytes.Buffer{}
	for i, tkn := range s.Items {
		if i == 0 {
			buf.WriteString(tkn.OriginString())
			continue
		}

		if tkn.HasPreSpace() {
			buf.WriteRune(' ')
		}

		buf.WriteString(tkn.OriginString())
	}

	return buf.String()
}

// one bnf expression
type Production struct {
	// serial Number of this production
	Number int
	// left value of bnf expression
	Head Token
	// right expression of bnf expression,
	// every Seq represents a branch of this expression
	Alter []*Seq
}

func NewProduction(head Token, pNumber int) (prod *Production, nextP int) {
	return &Production{Head: head, Number: pNumber}, pNumber + 1
}

func (p *Production) AppendSeq(s *Seq) {
	s.PNumber = p.Number
	s.SNumber = len(p.Alter)
	p.Alter = append(p.Alter, s)
}

const (
	initState            = 0
	delimFetchedState    = 1
	termFetchedState     = 2
	prepareNextProdState = 3
	endState             = 4
)

func skipComment(nextToken func() (Token, error)) (t Token, err error) {
	for {
		t, err = nextToken()
		if err != nil {
			return nil, err
		}

		if !isComment(t) {
			return t, nil
		}
	}
}

func collectHeadCodeBlocks(nextToken func() (Token, error)) (t Token, cbs []*CodeBlock, err error) {
	cbs = make([]*CodeBlock, 0)
	for {
		t, err = skipComment(nextToken)
		if err != nil {
			return nil, nil, err
		}

		if cb, ok := t.(*CodeBlock); ok {
			cbs = append(cbs, cb)
		} else {
			break
		}
	}

	return t, cbs, nil
}

func Parse(nextToken func() (Token, error)) ([]*CodeBlock, []*Production, error) {
	var tkn Token
	var prods []*Production
	var p *Production
	// production serial Number
	pNumber := 0
	var lastTerm Token

	state := initState
	t, codeblocks, err := collectHeadCodeBlocks(nextToken)
	if err != nil {
		return nil, nil, err
	}
	if !IsTknNonTerminal(t) {
		return nil, nil, fmt.Errorf("%s is not nonterminal", t.OriginString())
	}

	p, pNumber = NewProduction(t, pNumber)
	s := NewSeq(nil)

	//
	// initState -> delimFetchedState -> termFetchedState ->...
	//
	for state != endState {
		tkn, err = skipComment(nextToken)
		if err != nil {
			return nil, nil, err
		}
		switch state {
		case initState:
			if tkn.OriginString() != ":" {
				return nil, nil, errors.New("expect ':'")
			}
			state = delimFetchedState
		case delimFetchedState:
			if isEOF(tkn) {
				if err := s.Append(&terminal{val: ""}); err != nil {
					return nil, nil, err
				}
				p.AppendSeq(s)
				prods = append(prods, p)
				state = endState
				continue
			}
			if tkn.OriginString() == "|" || isEOF(tkn) {
				// multi delimiter will have empty alter
				if err := s.Append(&terminal{val: ""}); err != nil {
					return nil, nil, err
				}
				p.AppendSeq(s)
				s = NewSeq(nil)
			} else if tkn.OriginString() == ":" {
				continue
			} else {
				state = termFetchedState
				if err := s.Append(tkn); err != nil {
					return nil, nil, err
				}
			}
			// state after first term fetched
		case termFetchedState:
			switch v := tkn.(type) {
			case *eof:
				p.AppendSeq(s)
				prods = append(prods, p)
				state = endState
			case *operator:
				if v.OriginString() == "|" {
					p.AppendSeq(s)
					s = NewSeq(nil)
				}
				if v.OriginString() == ":" {
					s = NewSeq([]Token{&terminal{val: ""}})
					p.AppendSeq(s)
					prods = append(prods, p)
					p, pNumber = NewProduction(s.Items[0], pNumber)
					s = NewSeq(nil)
				}
				state = delimFetchedState
			case *nonTerminal, *keyword, *terminal, *attribute, *CodeBlock:
				// record last term
				lastTerm = v
				state = prepareNextProdState
			}
			// if one branch has many Token, it will always in this state
		case prepareNextProdState:
			switch v := tkn.(type) {
			case *eof:
				if err := s.Append(lastTerm); err != nil {
					return nil, nil, err
				}
				p.AppendSeq(s)
				prods = append(prods, p)
				state = endState
			case *operator:
				if v.val == "|" {
					if err := s.Append(lastTerm); err != nil {
						return nil, nil, err
					}
					p.AppendSeq(s)
					s = NewSeq(nil)
				} else if v.val == ":" {
					// enter next bnf expression
					p.AppendSeq(s)
					s = NewSeq(nil)
					prods = append(prods, p)
					if !IsTknNonTerminal(lastTerm) {
						return nil, nil, fmt.Errorf("%s is not nonterminal \n %s",
							lastTerm.OriginString(), debug.Stack())
					}
					p, pNumber = NewProduction(lastTerm, pNumber)
				}
				state = delimFetchedState
			case *nonTerminal, *keyword, *terminal, *attribute, *CodeBlock:
				// push last tern in Seq
				if err := s.Append(lastTerm); err != nil {
					return nil, nil, err
				}
				lastTerm = v
			}
		}
	}
	return codeblocks, prods, nil
}
