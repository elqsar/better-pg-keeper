// Package plans reads PostgreSQL EXPLAIN (FORMAT JSON) output: it finds
// sequential scans, proposes index columns from their filters, and describes a
// plan in plain language. It has no database access, so it is easy to test.
package plans

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Node is one plan node, holding only the fields this package reads.
type Node struct {
	NodeType     string  `json:"Node Type"`
	Strategy     string  `json:"Strategy"`
	RelationName string  `json:"Relation Name"`
	Schema       string  `json:"Schema"`
	Alias        string  `json:"Alias"`
	IndexName    string  `json:"Index Name"`
	Filter       string  `json:"Filter"`
	IndexCond    string  `json:"Index Cond"`
	HashCond     string  `json:"Hash Cond"`
	JoinFilter   string  `json:"Join Filter"`
	SortKey      []any   `json:"Sort Key"`
	SortMethod   string  `json:"Sort Method"`
	PlanRows     float64 `json:"Plan Rows"`
	PlanWidth    float64 `json:"Plan Width"`
	TotalCost    float64 `json:"Total Cost"`
	Plans        []Node  `json:"Plans"`
}

// Parse returns the root node of EXPLAIN (FORMAT JSON) output.
func Parse(planJSON string) (*Node, error) {
	var doc []struct {
		Plan Node `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(planJSON), &doc); err != nil {
		return nil, fmt.Errorf("parsing plan: %w", err)
	}
	if len(doc) == 0 {
		return nil, fmt.Errorf("parsing plan: empty document")
	}
	return &doc[0].Plan, nil
}

// Walk calls fn for n and every node below it, depth first.
func (n *Node) Walk(fn func(node *Node)) {
	fn(n)
	for i := range n.Plans {
		n.Plans[i].Walk(fn)
	}
}

// UsesIndex reports whether any node scans the named index.
func (n *Node) UsesIndex(name string) bool {
	used := false
	n.Walk(func(node *Node) {
		if node.IndexName == name {
			used = true
		}
	})
	return used
}

// SeqScan is a sequential scan found in a plan.
type SeqScan struct {
	Schema   string  `json:"schema"`
	Table    string  `json:"table"`
	Filter   string  `json:"filter,omitempty"`
	PlanRows float64 `json:"plan_rows"`
	Cost     float64 `json:"cost"`
	// Columns are proposed index key columns, in order; empty when the filter
	// gives nothing an index could use.
	Columns []string `json:"columns,omitempty"`
}

// SeqScans returns every sequential scan in the plan with its proposed columns.
func (n *Node) SeqScans() []SeqScan {
	var scans []SeqScan
	n.Walk(func(node *Node) {
		if node.NodeType != "Seq Scan" || node.RelationName == "" {
			return
		}
		scans = append(scans, SeqScan{
			Schema:   node.Schema,
			Table:    node.RelationName,
			Filter:   node.Filter,
			PlanRows: node.PlanRows,
			Cost:     node.TotalCost,
			Columns:  IndexColumns(node.Filter),
		})
	})
	return scans
}

// maxEqualityColumns bounds proposed indexes: a wider btree costs more to
// maintain and rarely helps more than the first few selective columns.
const maxEqualityColumns = 3

var (
	// A column reference, optionally qualified and optionally cast:
	// status, o.status, "Status", (o.status)::text
	columnPattern = `\(?((?:"[^"]+"|[A-Za-z_][\w$]*)(?:\.(?:"[^"]+"|[A-Za-z_][\w$]*))?)\)?(?:::[\w ]+(?:\[\])?)?`
	// A parameter or constant: $1, ($1)::text, 'open'::text, 42, NULL-free.
	valuePattern = `(?:\(?\$\d+\)?(?:::[\w ]+(?:\[\])?)?|'(?:[^']|'')*'(?:::[\w ]+(?:\[\])?)?|-?\d+(?:\.\d+)?(?:::[\w ]+)?)`

	comparison = regexp.MustCompile(`^` + columnPattern + `\s*(=|<=|>=|<|>)\s*` + valuePattern + `$`)
	// col = ANY ($1) or col = ANY ('{a,b}'::text[])
	anyComparison = regexp.MustCompile(`^` + columnPattern + `\s*=\s*ANY\s*\(` + valuePattern + `\)$`)
	// Reversed comparison: $1 < col
	reversedComparison = regexp.MustCompile(`^` + valuePattern + `\s*(=|<=|>=|<|>)\s*` + columnPattern + `$`)
)

// IndexColumns proposes btree key columns from a scan filter such as
// ((o.status = $1) AND (o.created_at > $2)): equality columns first, then at
// most one range column. It returns nil when the filter has an OR at the top,
// or no term it can parse - a missing suggestion is better than a wrong one.
func IndexColumns(filter string) []string {
	terms, ok := splitAnd(filter)
	if !ok {
		return nil
	}

	var equality, ranges []string
	seen := make(map[string]bool)
	for _, term := range terms {
		col, op, ok := parseTerm(term)
		if !ok || seen[col] {
			continue
		}
		switch op {
		case "=":
			if len(equality) < maxEqualityColumns {
				equality = append(equality, col)
				seen[col] = true
			}
		default:
			ranges = append(ranges, col)
		}
	}

	cols := equality
	for _, col := range ranges {
		if !seen[col] {
			cols = append(cols, col)
			break
		}
	}
	return cols
}

// parseTerm reads one comparison of a column with a parameter or constant and
// returns the column name and "=" or "range".
func parseTerm(term string) (column, op string, ok bool) {
	term = stripParens(strings.TrimSpace(term))
	if m := anyComparison.FindStringSubmatch(term); m != nil {
		return columnName(m[1]), "=", true
	}
	if m := comparison.FindStringSubmatch(term); m != nil {
		return columnName(m[1]), opKind(m[2]), true
	}
	if m := reversedComparison.FindStringSubmatch(term); m != nil {
		return columnName(m[2]), opKind(m[1]), true
	}
	return "", "", false
}

func opKind(op string) string {
	if op == "=" {
		return "="
	}
	return "range"
}

// columnName drops a table qualifier and identifier quotes.
func columnName(ref string) string {
	if i := lastUnquotedDot(ref); i >= 0 {
		ref = ref[i+1:]
	}
	if strings.HasPrefix(ref, `"`) && strings.HasSuffix(ref, `"`) && len(ref) >= 2 {
		return strings.ReplaceAll(ref[1:len(ref)-1], `""`, `"`)
	}
	return ref
}

func lastUnquotedDot(s string) int {
	inQuote := false
	last := -1
	for i, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == '.' && !inQuote:
			last = i
		}
	}
	return last
}

// splitAnd splits a filter into its top-level AND terms. It reports false when
// the filter contains a top-level OR, or its parentheses do not balance.
func splitAnd(filter string) ([]string, bool) {
	filter = stripParens(strings.TrimSpace(filter))
	if filter == "" {
		return nil, false
	}

	var terms []string
	depth, start := 0, 0
	inQuote := false
	for i := 0; i < len(filter); i++ {
		switch c := filter[i]; {
		case c == '\'':
			inQuote = !inQuote
		case inQuote:
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth < 0 {
				return nil, false
			}
		case depth == 0 && hasWordAt(filter, i, " OR "):
			return nil, false
		case depth == 0 && hasWordAt(filter, i, " AND "):
			terms = append(terms, filter[start:i])
			start = i + len(" AND ")
			i += len(" AND ") - 1
		}
	}
	if depth != 0 || inQuote {
		return nil, false
	}
	terms = append(terms, filter[start:])
	return terms, true
}

func hasWordAt(s string, i int, word string) bool {
	return strings.HasPrefix(s[i:], word)
}

// stripParens removes parentheses that wrap the whole expression.
func stripParens(s string) string {
	for len(s) >= 2 && s[0] == '(' && s[len(s)-1] == ')' && wraps(s) {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

// wraps reports whether the opening parenthesis at s[0] closes at the end.
func wraps(s string) bool {
	depth := 0
	inQuote := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\'':
			inQuote = !inQuote
		case inQuote:
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 && i != len(s)-1 {
				return false
			}
		}
	}
	return depth == 0
}
