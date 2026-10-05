package main

import "strings"

type jsonScanner struct {
	text string
	at   int
}

func (j *jsonScanner) skipSpace() {
	for j.at < len(j.text) && strings.IndexByte(" \t\r\n", j.text[j.at]) >= 0 {
		j.at++
	}
}

func (j *jsonScanner) consume(c byte) bool {
	j.skipSpace()
	if j.at < len(j.text) && j.text[j.at] == c {
		j.at++
		return true
	}
	return false
}

func (j *jsonScanner) skipString() bool {
	if j.at >= len(j.text) || j.text[j.at] != '"' {
		return false
	}
	for j.at++; j.at < len(j.text); j.at++ {
		switch j.text[j.at] {
		case '\\':
			j.at++
		case '"':
			j.at++
			return true
		}
	}
	return false
}

func (j *jsonScanner) skipValue() bool {
	j.skipSpace()
	if j.at >= len(j.text) {
		return false
	}
	switch j.text[j.at] {
	case '"':
		return j.skipString()
	case '{', '[':
		depth := 0
		for j.at < len(j.text) {
			switch j.text[j.at] {
			case '"':
				if !j.skipString() {
					return false
				}
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					j.at++
					return true
				}
			}
			j.at++
		}
		return false
	default:
		start := j.at
		for j.at < len(j.text) && strings.IndexByte(",}] \t\r\n", j.text[j.at]) < 0 {
			j.at++
		}
		literal := j.text[start:j.at]
		switch literal {
		case "true", "false", "null":
			return true
		case "":
			return false
		}
		return strings.Trim(literal, "0123456789+-.eE") == "" && strings.ContainsAny(literal, "0123456789")
	}
}

func jsonObjectMembersRaw(text string) (map[string]string, bool) {
	j := jsonScanner{text: text}
	members := make(map[string]string, 8)
	if !j.consume('{') {
		return nil, false
	}
	if j.consume('}') {
		return members, true
	}
	for {
		j.skipSpace()
		keyStart := j.at
		if !j.skipString() {
			return nil, false
		}
		key := text[keyStart+1 : j.at-1]
		if !j.consume(':') {
			return nil, false
		}
		j.skipSpace()
		valueStart := j.at
		if !j.skipValue() {
			return nil, false
		}
		value := text[valueStart:j.at]
		if value[0] == '"' {
			value = value[1 : len(value)-1]
		}
		members[key] = value
		if j.consume(',') {
			continue
		}
		if j.consume('}') {
			return members, true
		}
		return nil, false
	}
}
