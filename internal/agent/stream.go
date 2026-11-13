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
	output    json.RawMessage
	isError   bool
	message   string
	logErr    error
	readErr   error
	malformed int
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

const maxLine = 4 << 20

func stringReader(s string) io.Reader { return strings.NewReader(s) }

// parseStream reads stream-json lines, logs them, reports activity and
// returns the last result event. Lines longer than maxLine are logged in full
// but not parsed, so they cannot hide a later result. Malformed JSON lines are
// skipped and counted; a read error is returned in readErr. Log write
// failures never stop the stream: the first one is kept in logErr.
func parseStream(r io.Reader, agent, dir string, log io.Writer, onActivity func(Activity)) streamResult {
	var res streamResult
	br := bufio.NewReaderSize(r, 64*1024)
	var buf []byte
	overflow := false
	logWrite := func(b []byte) {
		if log == nil || res.logErr != nil {
			return
		}
		if _, err := log.Write(b); err != nil {
			res.logErr = err
		}
	}
	for {
		frag, isPrefix, err := br.ReadLine()
		if len(frag) > 0 {
			logWrite(frag)
			if !overflow {
				if len(buf)+len(frag) > maxLine {
					overflow, buf = true, nil
				} else {
					buf = append(buf, frag...)
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				res.readErr = err
			}
			if len(buf) > 0 && !overflow {
				handleLine(&res, buf, agent, dir, onActivity)
			}
			return res
		}
		if isPrefix {
			continue
		}
		logWrite([]byte{'\n'})
		if !overflow {
			handleLine(&res, buf, agent, dir, onActivity)
		}
		buf, overflow = buf[:0], false
	}
}

func handleLine(res *streamResult, line []byte, agent, dir string, onActivity func(Activity)) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	var ev event
	if json.Unmarshal(line, &ev) != nil {
		res.malformed++
		return
	}
	switch ev.Type {
	case "assistant":
		if onActivity == nil {
			return
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
		res.isError, res.message, res.output = ev.IsError, ev.Result, nil
		if string(ev.Output) != "null" {
			res.output = ev.Output
		}
	}
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
