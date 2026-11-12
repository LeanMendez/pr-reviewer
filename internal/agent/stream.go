package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
)

type streamResult struct {
	output  json.RawMessage
	isError bool
	message string
}

type event struct {
	Type    string          `json:"type"`
	IsError bool            `json:"is_error"`
	Result  string          `json:"result"`
	Output  json.RawMessage `json:"structured_output"`
	Message struct {
		Content []struct {
			Type  string `json:"type"`
			Name  string `json:"name"`
			Input struct {
				FilePath string `json:"file_path"`
				Pattern  string `json:"pattern"`
			} `json:"input"`
		} `json:"content"`
	} `json:"message"`
}

func stringReader(s string) io.Reader { return strings.NewReader(s) }

// parseStream reads stream-json lines, logs them, reports activity and
// returns the last result event.
func parseStream(r io.Reader, agent, dir string, log io.Writer, onActivity func(Activity)) streamResult {
	var res streamResult
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if log != nil {
			log.Write(append(append([]byte(nil), line...), '\n'))
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev event
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		switch ev.Type {
		case "assistant":
			if onActivity == nil {
				continue
			}
			for _, c := range ev.Message.Content {
				if c.Type != "tool_use" {
					continue
				}
				if text := activityText(c.Name, c.Input.FilePath, c.Input.Pattern, dir); text != "" {
					onActivity(Activity{Agent: agent, Text: text})
				}
			}
		case "result":
			res = streamResult{output: nil, isError: ev.IsError, message: ev.Result}
			if string(ev.Output) != "null" {
				res.output = ev.Output
			}
		}
	}
	return res
}

func activityText(tool, file, pattern, dir string) string {
	switch tool {
	case "Read":
		if file == "" {
			return ""
		}
		if dir != "" {
			if rel, err := filepath.Rel(dir, file); err == nil && !strings.HasPrefix(rel, "..") {
				file = rel
			}
		}
		return "leyendo " + file
	case "Grep":
		return "buscando " + pattern
	case "Glob":
		return "listando " + pattern
	}
	return ""
}
