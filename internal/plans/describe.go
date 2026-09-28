package plans

import (
	"fmt"
	"strings"
)

// Describe explains a plan in short sentences, most expensive work first as it
// appears top-down. Row counts are the planner's estimates.
func Describe(root *Node) []string {
	var lines []string
	var walk func(n *Node, parentLoops float64)
	walk = func(n *Node, parentLoops float64) {
		if line := describeNode(n, parentLoops); line != "" {
			lines = append(lines, line)
		}
		// The inner side of a nested loop runs once per outer row.
		if n.NodeType == "Nested Loop" && len(n.Plans) == 2 {
			walk(&n.Plans[0], parentLoops)
			walk(&n.Plans[1], parentLoops*max(n.Plans[0].PlanRows, 1))
			return
		}
		for i := range n.Plans {
			walk(&n.Plans[i], parentLoops)
		}
	}
	walk(root, 1)
	return lines
}

func describeNode(n *Node, loops float64) string {
	rel := relation(n)
	times := ""
	if loops > 1 {
		times = fmt.Sprintf(", run ~%s times (once per outer row)", count(loops))
	}
	switch n.NodeType {
	case "Seq Scan":
		if n.Filter == "" {
			return fmt.Sprintf("Reads every row of %s (est. %s rows)%s.", rel, count(n.PlanRows), times)
		}
		return fmt.Sprintf("Reads every row of %s and keeps those matching %s (est. %s rows)%s. An index on the filtered columns would avoid reading the whole table.",
			rel, n.Filter, count(n.PlanRows), times)
	case "Index Scan", "Index Only Scan":
		kind := "Looks up"
		if n.NodeType == "Index Only Scan" {
			kind = "Looks up (from the index alone, without visiting the table)"
		}
		cond := ""
		if n.IndexCond != "" {
			cond = " where " + n.IndexCond
		}
		return fmt.Sprintf("%s rows of %s through index %s%s (est. %s rows)%s.", kind, rel, n.IndexName, cond, count(n.PlanRows), times)
	case "Bitmap Index Scan":
		return fmt.Sprintf("Collects matching row locations from index %s (est. %s rows)%s.", n.IndexName, count(n.PlanRows), times)
	case "Bitmap Heap Scan":
		return fmt.Sprintf("Fetches those rows from %s (est. %s rows)%s.", rel, count(n.PlanRows), times)
	case "Sort", "Incremental Sort":
		keys := sortKeys(n)
		return fmt.Sprintf("Sorts ~%s rows by %s. An index in this order could return rows pre-sorted.", count(n.PlanRows), keys)
	case "Hash Join":
		return fmt.Sprintf("Joins two inputs by building a hash table on one side (%s).", n.HashCond)
	case "Merge Join":
		return "Joins two inputs that are both sorted on the join key."
	case "Nested Loop":
		return fmt.Sprintf("For each of ~%s outer rows, looks up matching rows on the inner side.", count(max(firstChildRows(n), 1)))
	case "Aggregate", "HashAggregate", "GroupAggregate":
		if n.Strategy == "Plain" {
			return "Computes an aggregate over all input rows."
		}
		return fmt.Sprintf("Groups rows into ~%s groups.", count(n.PlanRows))
	case "Limit":
		return fmt.Sprintf("Stops after %s rows.", count(n.PlanRows))
	case "ModifyTable":
		return fmt.Sprintf("Writes the resulting rows to %s.", rel)
	}
	return ""
}

func relation(n *Node) string {
	if n.RelationName == "" {
		return "the input"
	}
	if n.Schema != "" && n.Schema != "public" {
		return n.Schema + "." + n.RelationName
	}
	return n.RelationName
}

func sortKeys(n *Node) string {
	keys := make([]string, 0, len(n.SortKey))
	for _, k := range n.SortKey {
		keys = append(keys, fmt.Sprint(k))
	}
	if len(keys) == 0 {
		return "its sort key"
	}
	return strings.Join(keys, ", ")
}

func firstChildRows(n *Node) float64 {
	if len(n.Plans) == 0 {
		return 0
	}
	return n.Plans[0].PlanRows
}

// count renders an estimate as e.g. "2.1M", "40k" or "12".
func count(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.1fB", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case v >= 1e4:
		return fmt.Sprintf("%.0fk", v/1e3)
	}
	return fmt.Sprintf("%.0f", v)
}
