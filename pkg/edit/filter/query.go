package filter

import (
	"regexp"
	"strings"

	"src.elv.sh/pkg/errutil"
	"src.elv.sh/pkg/parse"
	"src.elv.sh/pkg/parse/cmpd"
	"src.elv.sh/pkg/store/storedefs"
)

// CompileQuery parses a filter and converts it to a storedefs.TextQuery with
// the same semantics as the Filter returned by Compile, so that it can be
// evaluated by the storage daemon rather than in the editor.
func CompileQuery(q string) (storedefs.TextQuery, error) {
	qn, errParse := parseFilter(q)
	query, errCompile := compileQueryFilter(qn)
	return query, errutil.Multi(errParse, errCompile)
}

func compileQueryFilter(qn *parse.Filter) (storedefs.TextQuery, error) {
	if len(qn.Opts) > 0 {
		return storedefs.TextQuery{}, notSupportedError{"option"}
	}
	subs, err := compileQueryCompounds(qn.Args)
	if err != nil {
		return storedefs.TextQuery{}, err
	}
	return storedefs.TextQuery{Op: storedefs.QAnd, Subs: subs}, nil
}

func compileQueryCompounds(ns []*parse.Compound) ([]storedefs.TextQuery, error) {
	qs := make([]storedefs.TextQuery, len(ns))
	for i, n := range ns {
		q, err := compileQueryCompound(n)
		if err != nil {
			return nil, err
		}
		qs[i] = q
	}
	return qs, nil
}

func compileQueryCompound(n *parse.Compound) (storedefs.TextQuery, error) {
	if pn, ok := cmpd.Primary(n); ok {
		switch pn.Type {
		case parse.Bareword, parse.SingleQuoted, parse.DoubleQuoted:
			s := pn.Value
			return storedefs.TextQuery{
				Op: storedefs.QSubstring, Arg: s, Fold: s == strings.ToLower(s)}, nil
		case parse.List:
			return compileQueryList(pn.Elements)
		}
	}
	return storedefs.TextQuery{}, notSupportedError{cmpd.Shape(n)}
}

func compileQueryList(elems []*parse.Compound) (storedefs.TextQuery, error) {
	if len(elems) == 0 {
		return storedefs.TextQuery{}, errEmptySubfilter
	}
	head, ok := cmpd.StringLiteral(elems[0])
	if !ok {
		return storedefs.TextQuery{}, notSupportedError{"non-literal subfilter head"}
	}
	switch head {
	case "re":
		if len(elems) == 1 {
			return storedefs.TextQuery{}, notSupportedError{"re subfilter with no argument"}
		}
		if len(elems) > 2 {
			return storedefs.TextQuery{}, notSupportedError{"re subfilter with two or more arguments"}
		}
		arg := elems[1]
		s, ok := cmpd.StringLiteral(arg)
		if !ok {
			return storedefs.TextQuery{}, notSupportedError{"re subfilter with " + cmpd.Shape(arg)}
		}
		// Validate here, so that errors are reported where the filter is
		// typed rather than by the daemon.
		if _, err := regexp.Compile(s); err != nil {
			return storedefs.TextQuery{}, err
		}
		return storedefs.TextQuery{Op: storedefs.QRegexp, Arg: s}, nil
	case "and", "or":
		subs, err := compileQueryCompounds(elems[1:])
		if err != nil {
			return storedefs.TextQuery{}, err
		}
		op := storedefs.QAnd
		if head == "or" {
			op = storedefs.QOr
		}
		return storedefs.TextQuery{Op: op, Subs: subs}, nil
	default:
		return storedefs.TextQuery{}, notSupportedError{"head " + parse.SourceText(elems[0])}
	}
}
