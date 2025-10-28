package opper

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// JSONChunkAggregator combines streaming JSON chunks indexed by JSONPath into a
// final JSON document. It mirrors the helper used by the reference Opper agent
// implementation so we can decode partial streaming payloads.
type JSONChunkAggregator struct {
	chunks map[string]*strings.Builder
}

func NewJSONChunkAggregator() *JSONChunkAggregator {
	return &JSONChunkAggregator{chunks: make(map[string]*strings.Builder)}
}

// Add appends a JSON path delta to the aggregator.
func (a *JSONChunkAggregator) Add(path, delta string) {
	if path == "" || delta == "" {
		return
	}
	b := a.chunks[path]
	if b == nil {
		b = &strings.Builder{}
		a.chunks[path] = b
	}
	b.WriteString(delta)
}

// Assemble marshals the aggregated chunk data into a JSON string.
func (a *JSONChunkAggregator) Assemble() (string, error) {
	if len(a.chunks) == 0 {
		return "", nil
	}
	root := map[string]any{}
	paths := make([]string, 0, len(a.chunks))
	for path := range a.chunks {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := assignJSONPath(root, path, a.chunks[path].String()); err != nil {
			return "", err
		}
	}
	data, err := json.Marshal(root)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

type jsonPathToken struct {
	key   string
	index int
}

func parseJSONPath(path string) ([]jsonPathToken, error) {
	if path == "" {
		return nil, fmt.Errorf("empty json path")
	}
	tokens := []jsonPathToken{}
	for i := 0; i < len(path); {
		start := i
		for i < len(path) && path[i] != '.' && path[i] != '[' {
			i++
		}
		if start == i {
			return nil, fmt.Errorf("invalid json path segment in %q", path)
		}
		tok := jsonPathToken{key: path[start:i], index: -1}
		if i < len(path) && path[i] == '[' {
			i++
			startIdx := i
			for i < len(path) && path[i] != ']' {
				i++
			}
			if i >= len(path) {
				return nil, fmt.Errorf("unmatched '[' in json path %q", path)
			}
			idx, err := strconv.Atoi(path[startIdx:i])
			if err != nil {
				return nil, fmt.Errorf("invalid array index in json path %q", path)
			}
			tok.index = idx
			i++
		}
		tokens = append(tokens, tok)
		if i < len(path) && path[i] == '.' {
			i++
		}
	}
	return tokens, nil
}

func assignJSONPath(root map[string]any, path string, value string) error {
	tokens, err := parseJSONPath(path)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return fmt.Errorf("empty token list for path %q", path)
	}
	current := root
	for i, tok := range tokens {
		isLast := i == len(tokens)-1
		if tok.index >= 0 {
			node, exists := current[tok.key]
			var arr []any
			if !exists {
				arr = make([]any, tok.index+1)
				current[tok.key] = arr
			} else {
				var ok bool
				arr, ok = node.([]any)
				if !ok {
					return fmt.Errorf("path %q expected array at %q", path, tok.key)
				}
				if tok.index >= len(arr) {
					newArr := make([]any, tok.index+1)
					copy(newArr, arr)
					arr = newArr
					current[tok.key] = arr
				}
			}
			if isLast {
				arr[tok.index] = value
				current[tok.key] = arr
				return nil
			}
			child := arr[tok.index]
			childMap, ok := child.(map[string]any)
			if !ok || childMap == nil {
				childMap = map[string]any{}
				arr[tok.index] = childMap
				current[tok.key] = arr
			}
			current = childMap
			continue
		}
		if isLast {
			current[tok.key] = value
			return nil
		}
		node, exists := current[tok.key]
		if !exists {
			child := map[string]any{}
			current[tok.key] = child
			current = child
			continue
		}
		childMap, ok := node.(map[string]any)
		if !ok {
			return fmt.Errorf("path %q expected object at %q", path, tok.key)
		}
		current = childMap
	}
	return nil
}
