package plans

import (
	"reflect"
	"strings"
	"testing"
)

func TestIndexColumns(t *testing.T) {
	tests := []struct {
		filter string
		want   []string
	}{
		{"(orders.status = $1)", []string{"status"}},
		{"((o.status = $1) AND (o.created_at > $2))", []string{"status", "created_at"}},
		// Equality columns come before the range column whatever the written order.
		{"((o.created_at >= $1) AND (o.customer_id = $2) AND (o.status = 'open'::text))", []string{"customer_id", "status", "created_at"}},
		// Only one range column is useful in a btree.
		{"((o.a > $1) AND (o.b < $2))", []string{"a"}},
		// Casts on either side, as VERBOSE prints them for varchar columns.
		{"((o.status)::text = ($1)::text)", []string{"status"}},
		{"(o.id = ANY ($1))", []string{"id"}},
		{"(o.kind = ANY ('{a,b}'::text[]))", []string{"kind"}},
		{"($1 < o.amount)", []string{"amount"}},
		{`("Order"."Status" = $1)`, []string{"Status"}},
		// Unparseable terms are skipped; the rest still counts.
		{"((lower(o.email) = $1) AND (o.tenant_id = $2))", []string{"tenant_id"}},
		{"((o.note ~~ 'x%'::text) AND (o.deleted_at IS NULL))", nil},
		// A top-level OR can't be served by one btree.
		{"((o.a = $1) OR (o.b = $2))", nil},
		// OR nested inside one AND term is just an unparseable term.
		{"((o.tenant_id = $1) AND ((o.a = $2) OR (o.b = $3)))", []string{"tenant_id"}},
		// Column-to-column comparisons are join conditions, not index keys here.
		{"(o.a = o.b)", nil},
		// AND inside a string constant is not a separator.
		{"(o.note = 'x AND y'::text)", []string{"note"}},
		{"", nil},
	}
	for _, tt := range tests {
		if got := IndexColumns(tt.filter); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("IndexColumns(%q) = %v, want %v", tt.filter, got, tt.want)
		}
	}
}

// A PG17 generic plan (VERBOSE, FORMAT JSON) of a join, trimmed to the fields read.
const joinPlan = `[{"Plan": {
  "Node Type": "Sort", "Plan Rows": 40000, "Total Cost": 9000, "Sort Key": ["o.created_at DESC"],
  "Plans": [{
    "Node Type": "Nested Loop", "Plan Rows": 40000, "Total Cost": 8000,
    "Plans": [
      {"Node Type": "Seq Scan", "Relation Name": "orders", "Schema": "public", "Alias": "o",
       "Filter": "((o.status = $1) AND (o.created_at > $2))", "Plan Rows": 30, "Total Cost": 4500},
      {"Node Type": "Index Scan", "Relation Name": "customers", "Schema": "sales", "Alias": "c",
       "Index Name": "customers_pkey", "Index Cond": "(c.id = o.customer_id)", "Plan Rows": 1, "Total Cost": 8}
    ]
  }]
}}]`

func TestSeqScansAndDescribe(t *testing.T) {
	root, err := Parse(joinPlan)
	if err != nil {
		t.Fatal(err)
	}

	scans := root.SeqScans()
	if len(scans) != 1 {
		t.Fatalf("scans = %+v", scans)
	}
	s := scans[0]
	if s.Schema != "public" || s.Table != "orders" || s.Cost != 4500 || !reflect.DeepEqual(s.Columns, []string{"status", "created_at"}) {
		t.Errorf("scan = %+v", s)
	}
	if !root.UsesIndex("customers_pkey") || root.UsesIndex("nope") {
		t.Error("UsesIndex")
	}

	lines := Describe(root)
	text := strings.Join(lines, "\n")
	for _, want := range []string{
		"Sorts ~40k rows by o.created_at DESC",
		"For each of ~30 outer rows",
		"Reads every row of orders and keeps those matching",
		"through index customers_pkey",
		"sales.customers",
		"run ~30 times",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("description missing %q:\n%s", want, text)
		}
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "[]", "not json"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) should fail", in)
		}
	}
}
