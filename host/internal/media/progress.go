package media

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Progress struct {
	Stage      string
	Percent    float64
	BytesDone  int64
	BytesTotal int64
	SpeedBps   float64
	EtaSec     *float64
	// File is the latest file the download reported writing to.
	File string
	// OutputPath is the finished file. Final is set once yt-dlp has confirmed it.
	OutputPath string
	Final      bool
	SawMerger  bool
}

// FilePrefix marks the line on which yt-dlp prints the finished file's path.
const FilePrefix = "streamgrab-file:"

func ParseLine(line string, current Progress) Progress {
	line = strings.TrimSpace(stripANSI(line))
	switch {
	case strings.HasPrefix(line, FilePrefix):
		if path := strings.TrimPrefix(line, FilePrefix); path != "" {
			current.OutputPath = path
			current.Final = true
		}
	case strings.HasPrefix(line, "streamgrab:"):
		return parseTemplate(line, current)
	case strings.HasPrefix(line, "[Merger]"):
		current = postprocessing(current, quotedPath(line))
	case strings.HasPrefix(line, "[ExtractAudio] Destination:"):
		current = postprocessing(current, strings.TrimSpace(strings.TrimPrefix(line, "[ExtractAudio] Destination:")))
	case strings.HasPrefix(line, "[ExtractAudio]"):
		current = postprocessing(current, "")
	case strings.HasPrefix(line, "[download] Destination:"):
		current = advanceStage(current)
		if path := strings.TrimSpace(strings.TrimPrefix(line, "[download] Destination:")); path != "" {
			current.File = path
		}
	case strings.HasPrefix(line, "[ThumbnailsConvertor] Converting thumbnail"):
		// Thumbnails are recorded so that cancelling a job removes them.
		if path := quotedPath(line); path != "" {
			current.File = strings.TrimSuffix(path, filepath.Ext(path)) + ".jpg"
		}
	default:
		if path := thumbnailPath(line); path != "" {
			current.File = path
		}
	}
	return current
}

// thumbnailWritten matches yt-dlp's notice that it saved a thumbnail.
var thumbnailWritten = regexp.MustCompile(`^\[[^\]]+\] (?:[^:]+: )?Writing (?:[^:]* )?thumbnail(?: [^:]*)? to: (.+\.(?:jpg|jpeg|png|webp))$`)

func thumbnailPath(line string) string {
	if match := thumbnailWritten.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	return ""
}

func postprocessing(current Progress, path string) Progress {
	current.Stage = "muxing"
	current.SawMerger = true
	current.Percent = 100
	if path != "" {
		current.File = path
		if !current.Final {
			current.OutputPath = path
		}
	}
	return current
}

func parseTemplate(line string, current Progress) Progress {
	parts := strings.Split(strings.TrimPrefix(line, "streamgrab:"), ":")
	if len(parts) < 5 {
		return current
	}
	done := parseInt(parts[0])
	total := parseInt(parts[1])
	if total == 0 {
		total = parseInt(parts[2])
	}
	speed := parseFloat(parts[3])
	eta := parseFloat(parts[4])
	if current.Stage == "" || current.Stage == "queued" {
		current.Stage = "downloading_video"
	}
	if current.Stage == "muxing" {
		return current
	}
	current.BytesDone = done
	if total > 0 {
		current.BytesTotal = total
		current.Percent = float64(done) / float64(total) * 100
		if current.Percent > 100 {
			current.Percent = 100
		}
	}
	if speed > 0 {
		current.SpeedBps = speed
	}
	if eta > 0 {
		value := eta
		current.EtaSec = &value
	} else {
		current.EtaSec = nil
	}
	return current
}

func advanceStage(current Progress) Progress {
	if current.Stage == "downloading_video" && current.Percent >= 99 {
		current.Stage = "downloading_audio"
		current.Percent = 0
		current.BytesDone = 0
		current.BytesTotal = 0
		return current
	}
	if current.Stage == "" || current.Stage == "queued" {
		current.Stage = "downloading_video"
	}
	return current
}

func quotedPath(line string) string {
	start := strings.Index(line, "\"")
	end := strings.LastIndex(line, "\"")
	if start >= 0 && end > start {
		return line[start+1 : end]
	}
	return ""
}

func parseInt(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" || value == "NA" || value == "None" {
		return 0
	}
	n, _ := strconv.ParseInt(strings.Split(value, ".")[0], 10, 64)
	return n
}

func parseFloat(value string) float64 {
	value = strings.TrimSpace(value)
	if value == "" || value == "NA" || value == "None" {
		return 0
	}
	n, _ := strconv.ParseFloat(value, 64)
	return n
}

func stripANSI(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == 0x1b {
			for i < len(value) && value[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(value[i])
	}
	return b.String()
}
