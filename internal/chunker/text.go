package chunker

import (
	"strings"

	"github.com/Hendrixx-RE/Vektix/internal/config"
	"github.com/Hendrixx-RE/Vektix/internal/ollama"
	"github.com/Hendrixx-RE/Vektix/internal/store"
)

func normalizeConfig(cfg config.ChunkingConfig) config.ChunkingConfig {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 256
		if cfg.OverlapTokens == 0 {
			cfg.OverlapTokens = 50
		}
		if cfg.MinTokens == 0 {
			cfg.MinTokens = 20
		}
	}
	if cfg.OverlapTokens < 0 {
		cfg.OverlapTokens = 0
	}
	if cfg.OverlapTokens >= cfg.MaxTokens {
		cfg.OverlapTokens = cfg.MaxTokens / 4
	}
	return cfg
}

type fragment struct {
	text      string
	tokens    int
	startLine int
	endLine   int
	isHeading bool
}

func getFragments(content string) []fragment {
	var frags []fragment
	start := 0
	startLine := 1
	currLine := 1

	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			currLine++
		}

		split := false
		endIdx := i + 1

		if i+1 < len(content) && content[i] == '\n' && content[i+1] == '\n' {
			split = true
			endIdx = i + 2
			currLine++
			i++
		} else if i+1 < len(content) && content[i] == '.' && content[i+1] == ' ' {
			split = true
			endIdx = i + 2
			i++
		} else if i+1 < len(content) && content[i] == '\n' && content[i+1] == '#' {
			split = true
			endIdx = i + 1
		} else if i == len(content)-1 {
			split = true
			endIdx = i + 1
		}

		if split {
			text := content[start:endIdx]
			if text != "" {
				isH := strings.HasPrefix(strings.TrimLeft(text, " \t"), "#")
				frags = append(frags, fragment{
					text:      text,
					tokens:    ollama.EstimateTokens(text),
					startLine: startLine,
					endLine:   currLine,
					isHeading: isH,
				})
			}
			start = endIdx
			startLine = currLine
		}
	}
	return frags
}

func splitOversized(frags []fragment, cfg config.ChunkingConfig) []fragment {
	var res []fragment
	for _, f := range frags {
		if f.tokens <= cfg.MaxTokens {
			res = append(res, f)
			continue
		}
		
		// Split by space
		words := strings.SplitAfter(f.text, " ")
		var currText string
		currTokens := 0
		currStart := f.startLine
		// We'll estimate lines by counting \n in the words (though sentences rarely have \n, they might)
		
		for _, w := range words {
			wt := ollama.EstimateTokens(w)
			if currTokens+wt > cfg.MaxTokens && currTokens > 0 {
				lines := strings.Count(currText, "\n")
				res = append(res, fragment{
					text:      currText,
					tokens:    currTokens,
					startLine: currStart,
					endLine:   currStart + lines,
					isHeading: strings.HasPrefix(strings.TrimLeft(currText, " \t"), "#"),
				})
				currStart += lines
				currText = w
				currTokens = wt
			} else {
				currText += w
				currTokens += wt
			}
		}
		if currText != "" {
			lines := strings.Count(currText, "\n")
			res = append(res, fragment{
				text:      currText,
				tokens:    currTokens,
				startLine: currStart,
				endLine:   currStart + lines,
				isHeading: strings.HasPrefix(strings.TrimLeft(currText, " \t"), "#"),
			})
		}
	}
	return res
}

func ChunkText(path, content string, cfg config.ChunkingConfig) []store.Chunk {
	cfg = normalizeConfig(cfg)
	var chunks []store.Chunk
	if content == "" {
		return chunks
	}

	frags := getFragments(content)
	frags = splitOversized(frags, cfg)

	if len(frags) == 0 {
		return chunks
	}

	i := 0
	for i < len(frags) {
		var chunkText strings.Builder
		chunkTokens := 0
		startLine := frags[i].startLine
		endLine := frags[i].endLine

		j := i
		stoppedForHeading := false
		headingThreshold := cfg.MaxTokens - cfg.OverlapTokens - 50
		if headingThreshold < 0 {
			headingThreshold = 0
		}
		for ; j < len(frags); j++ {
			f := frags[j]

			// Prefer heading boundary if we already have a decent chunk size
			if j > i && f.isHeading && chunkTokens >= headingThreshold {
				stoppedForHeading = true
				break
			}

			if chunkTokens+f.tokens > cfg.MaxTokens && chunkTokens > 0 {
				break
			}

			chunkText.WriteString(f.text)
			chunkTokens += f.tokens
			endLine = f.endLine
		}

		if chunkTokens >= cfg.MinTokens || (j == len(frags) && len(chunks) == 0) {
			chunks = append(chunks, store.Chunk{
				Path:    path,
				Content: chunkText.String(),
				Locator: store.Locator{
					Kind:  store.LocatorLineRange,
					Start: startLine,
					End:   endLine,
				},
			})
		}

		if j == len(frags) {
			break
		}

		if stoppedForHeading {
			i = j
			continue
		}

		overlapSum := 0
		overlapIdx := j - 1
		for overlapIdx > i {
			if frags[overlapIdx].isHeading {
				break
			}
			overlapSum += frags[overlapIdx].tokens
			if overlapSum >= cfg.OverlapTokens {
				break
			}
			overlapIdx--
		}

		if overlapIdx <= i {
			overlapIdx = i + 1
		}
		i = overlapIdx
	}

	return chunks
}
