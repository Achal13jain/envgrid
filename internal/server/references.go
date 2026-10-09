package server

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Achal13jain/envgrid/internal/formats"
	"github.com/Achal13jain/envgrid/internal/store"
)

// refPattern matches ${NAME}. NAME is a key of the same file, either in the
// same environment (${API_URL}) or in another one (${prod.API_URL}).
var refPattern = regexp.MustCompile(`\$\{([A-Za-z0-9_.-]+)\}`)

// maxExpandedBytes caps everything one export's references expand to.
const maxExpandedBytes = 4 << 20

// resolveReferences returns one environment's values with references filled
// in. A reference to a key or environment that does not exist, or to a
// missing value, is left exactly as written, so shell-style ${HOME} survives.
// Cycles are errors, named by key, never by value.
//
// Two guards keep members from steering a protected export: a key of the
// same environment wins over an environment prefix (${db.host} with a key
// db.host), and a protected export never reads an unprotected environment.
// One budget covers every value read for a nested reference and every
// expanded copy, since one value can refer to another many times and the
// work multiplies with depth even when the values are empty.
func resolveReferences(g *store.Grid, envID int64) ([]formats.KV, error) {
	envs := map[string]int{}
	col := -1
	for i, e := range g.Environments {
		envs[e.Name] = i
		if e.ID == envID {
			col = i
		}
	}
	rows := map[string]int{}
	for i, r := range g.Rows {
		rows[r.Key.Name] = i
	}
	present := func(c int, key string) (string, bool) {
		ri, ok := rows[key]
		if !ok || !g.Rows[ri].Cells[c].Present {
			return "", false
		}
		return g.Rows[ri].Cells[c].Plaintext, true
	}

	budget := maxExpandedBytes
	tooBig := fmt.Errorf("references expand to more than %d MiB", maxExpandedBytes>>20)
	var expand func(raw string, c int, stack []string) (string, error)
	expand = func(raw string, c int, stack []string) (string, error) {
		var firstErr error
		out := refPattern.ReplaceAllStringFunc(raw, func(m string) string {
			if budget < 0 {
				return m
			}
			name := m[2 : len(m)-1]
			target, key := c, name
			_, own := present(c, name)
			if dot := strings.IndexByte(name, '.'); dot > 0 && !own {
				if ei, ok := envs[name[:dot]]; ok && (g.Environments[ei].IsProtected || !g.Environments[col].IsProtected) {
					if _, ok := present(ei, name[dot+1:]); ok {
						target, key = ei, name[dot+1:]
					}
				}
			}
			value, ok := present(target, key)
			if !ok {
				return m
			}
			here := fmt.Sprintf("%s in %s", key, g.Environments[target].Name)
			for _, seen := range stack {
				if seen == here {
					if firstErr == nil {
						firstErr = fmt.Errorf("references form a loop through %s", here)
					}
					return m
				}
			}
			if len(stack) >= 20 {
				if firstErr == nil {
					firstErr = fmt.Errorf("references are nested more than 20 deep at %s", here)
				}
				return m
			}
			if budget -= len(value) + 1; budget < 0 {
				if firstErr == nil {
					firstErr = tooBig
				}
				return m
			}
			resolved, err := expand(value, target, append(stack, here))
			if err != nil && firstErr == nil {
				firstErr = err
			}
			if budget -= len(resolved); budget < 0 && firstErr == nil {
				firstErr = tooBig
			}
			return resolved
		})
		return out, firstErr
	}

	kvs := []formats.KV{}
	if col < 0 {
		return kvs, nil
	}
	for _, r := range g.Rows {
		cell := r.Cells[col]
		if !cell.Present {
			continue
		}
		v, err := expand(cell.Plaintext, col, []string{fmt.Sprintf("%s in %s", r.Key.Name, g.Environments[col].Name)})
		if err != nil {
			return nil, err
		}
		kvs = append(kvs, formats.KV{Key: r.Key.Name, Value: v})
	}
	return kvs, nil
}
