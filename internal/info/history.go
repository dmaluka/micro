package info

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"strings"

	"github.com/zyedidia/micro/v2/internal/config"
	"github.com/zyedidia/micro/v2/internal/util"
)

// LoadHistory attempts to load user history from configDir/buffers/history
// into the history map
// The savehistory option must be on
func (i *InfoBuf) LoadHistory() {
	if config.GetGlobalOption("savehistory").(bool) {
		file, err := os.Open(filepath.Join(config.ConfigDir, "buffers", "history"))
		defer file.Close()
		var decodedMap map[string][]string
		if err == nil {
			decoder := gob.NewDecoder(file)
			err = decoder.Decode(&decodedMap)

			if err != nil {
				i.Error("Error loading history:", err)
				return
			}
		}

		if decodedMap != nil {
			i.History = decodedMap
		} else {
			i.History = make(map[string][]string)
		}
	} else {
		i.History = make(map[string][]string)
	}
}

// SaveHistory saves the user's command history to configDir/buffers/history
// only if the savehistory option is on
func (i *InfoBuf) SaveHistory() {
	if config.GetGlobalOption("savehistory").(bool) {
		// Don't save history past 100
		for k, v := range i.History {
			if len(v) > 100 {
				i.History[k] = v[len(i.History[k])-100:]
			}
		}

		file, err := os.Create(filepath.Join(config.ConfigDir, "buffers", "history"))
		defer file.Close()
		if err == nil {
			encoder := gob.NewEncoder(file)

			err = encoder.Encode(i.History)
			if err != nil {
				i.Error("Error saving history:", err)
				return
			}
		}
	}
}

// UpHistory fetches the previous item in the history
func (i *InfoBuf) UpHistory(history []string) {
	if i.HistoryNum > 0 && i.HasPrompt && !i.HasYN {
		i.HistoryNum--
		i.Replace(i.Start(), i.End(), history[i.HistoryNum])
		i.Buffer.GetActiveCursor().GotoLoc(i.End())
	}
}

// DownHistory fetches the next item in the history
func (i *InfoBuf) DownHistory(history []string) {
	if i.HistoryNum < len(history)-1 && i.HasPrompt && !i.HasYN {
		i.HistoryNum++
		i.Replace(i.Start(), i.End(), history[i.HistoryNum])
		i.Buffer.GetActiveCursor().GotoLoc(i.End())
	}
}

// SearchUpHistory fetches the previous item in the history
// beginning with the text in the infobuffer before cursor
func (i *InfoBuf) SearchUpHistory(history []string) {
	if i.HistoryNum > 0 && i.HasPrompt && !i.HasYN {
		i.searchHistory(history, false)
	}
}

// SearchDownHistory fetches the next item in the history
// beginning with the text in the infobuffer before cursor
func (i *InfoBuf) SearchDownHistory(history []string) {
	if i.HistoryNum < len(history)-1 && i.HasPrompt && !i.HasYN {
		i.searchHistory(history, true)
	}
}

func (i *InfoBuf) searchHistory(history []string, down bool) {
	line := string(i.LineBytes(0))
	c := i.Buffer.GetActiveCursor()

	if !i.HistorySearch || !strings.HasPrefix(line, i.HistorySearchPrefix) {
		i.HistorySearch = true
		i.HistorySearchPrefix = util.SliceStartStr(line, c.X)
	}

	found := -1
	if down {
		for j := i.HistoryNum + 1; j < len(history); j++ {
			if strings.HasPrefix(history[j], i.HistorySearchPrefix) {
				found = j
				break
			}
		}
	} else {
		for j := i.HistoryNum - 1; j >= 0; j-- {
			if strings.HasPrefix(history[j], i.HistorySearchPrefix) {
				found = j
				break
			}
		}
	}
	if found != -1 {
		i.HistoryNum = found
		i.Replace(i.Start(), i.End(), history[found])
		c.GotoLoc(i.End())
	}
}
