package plans

import (
	"strings"
)

// maxIdentifierBytes is PostgreSQL's NAMEDATALEN - 1; longer names are truncated.
const maxIdentifierBytes = 63

// IndexName proposes a name like idx_orders_status_created_at, shortened to
// fit PostgreSQL's identifier limit.
func IndexName(table string, columns []string) string {
	name := "idx_" + table + "_" + strings.Join(columns, "_")
	name = strings.Map(func(r rune) rune {
		if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, strings.ToLower(name))
	if len(name) > maxIdentifierBytes {
		name = name[:maxIdentifierBytes]
	}
	return name
}

// CreateIndexSQL builds a CREATE INDEX statement. With concurrently it uses
// CREATE INDEX CONCURRENTLY, which does not block writes. An empty name lets
// PostgreSQL choose one (hypopg needs no name).
func CreateIndexSQL(schema, table string, columns []string, name string, concurrently bool) string {
	var b strings.Builder
	b.WriteString("CREATE INDEX ")
	if concurrently {
		b.WriteString("CONCURRENTLY ")
	}
	if name != "" {
		b.WriteString(QuoteIdent(name))
		b.WriteString(" ")
	}
	b.WriteString("ON ")
	b.WriteString(QuoteIdent(schema))
	b.WriteString(".")
	b.WriteString(QuoteIdent(table))
	b.WriteString(" (")
	for i, c := range columns {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(QuoteIdent(c))
	}
	b.WriteString(")")
	return b.String()
}

// QuoteIdent quotes an SQL identifier.
func QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// HasPrefix reports whether index key columns start with all of prefix.
func HasPrefix(columns, prefix []string) bool {
	if len(prefix) > len(columns) {
		return false
	}
	for i := range prefix {
		if columns[i] != prefix[i] {
			return false
		}
	}
	return true
}
