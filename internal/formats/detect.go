package formats

import (
	"bytes"
	"encoding/json"
	"path"
	"regexp"
	"strings"
)

var (
	phpMarker = regexp.MustCompile(`(?m)<\?php|^[ \t]*(?:(?:public|protected|private|final)[ \t]+)*const[ \t]+[A-Za-z_]|\bdefine[ \t]*\([ \t]*['"]`)
	eqLine    = regexp.MustCompile(`^(export[ \t]+)?([^=:\s#!][^=:]*?)[ \t]*=`)
	colonLine = regexp.MustCompile(`^(- |[^=\s#!][^=]*?:([ \t]|$))`)
	upperKey  = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	iniHeader = regexp.MustCompile(`(?m)^[ \t]*\[[^\]\n]+\][ \t]*\r?$`)
)

// FromName guesses a format from a file name, or "" when it says nothing.
func FromName(name string) string {
	n := strings.ToLower(path.Base(strings.ReplaceAll(name, `\`, "/")))
	switch {
	case n == ".env" || strings.HasSuffix(n, ".env") || strings.HasPrefix(n, ".env."):
		return Dotenv
	case strings.HasSuffix(n, ".json"):
		return JSON
	case strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml"):
		return YAML
	case strings.HasSuffix(n, ".properties"):
		return Properties
	case strings.HasSuffix(n, ".ini") || strings.HasSuffix(n, ".cfg"):
		return INI
	case strings.HasSuffix(n, ".php"):
		return PHP
	case strings.HasSuffix(n, ".csv"):
		return CSV
	}
	return ""
}

// parses reports whether body reads as format with at least one key.
func parses(format string, body []byte) bool {
	kvs, err := Parse(format, body)
	return err == nil && len(kvs) > 0
}

// Detect guesses the format of a file from its name and content. A name
// with a known extension wins when the content reads that way; otherwise the
// content decides. Anything unrecognised, such as prose or an nginx config,
// is raw: kept whole, as plain text.
func Detect(name string, body []byte) string {
	if f := FromName(name); f != "" && parses(f, body) {
		return f
	}
	text := strings.TrimSpace(strings.TrimPrefix(string(body), "\xef\xbb\xbf"))
	if text == "" {
		return Raw
	}
	if text[0] == '{' && json.Valid([]byte(text)) {
		return JSON
	}
	if phpMarker.MatchString(text) && parses(PHP, body) {
		return PHP
	}
	if iniHeader.MatchString(text) && parses(INI, body) {
		return INI
	}
	if looksLikeCSV(text) {
		return CSV
	}

	var eq, colon, propsHints, envHints int
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || line[0] == '#':
			continue
		case line[0] == '!':
			propsHints++
			continue
		}
		if strings.HasSuffix(line, `\`) {
			propsHints++
		}
		if m := eqLine.FindStringSubmatch(line); m != nil {
			eq++
			key := strings.TrimSpace(m[2])
			switch {
			case m[1] != "" || upperKey.MatchString(key):
				envHints++
			case strings.Contains(key, ".") && strings.ToLower(key) == key:
				propsHints++
			}
			continue
		}
		if colonLine.MatchString(line) {
			colon++
		}
	}
	if colon > eq && parses(YAML, body) {
		return YAML
	}
	if eq > 0 {
		order := []string{Dotenv, Properties}
		if propsHints > envHints {
			order = []string{Properties, Dotenv}
		}
		for _, f := range order {
			if parses(f, body) {
				return f
			}
		}
	}
	if bytes.ContainsRune(body, ':') && parses(YAML, body) {
		return YAML
	}
	return Raw
}
