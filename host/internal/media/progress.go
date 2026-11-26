package media

import (
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
	OutputPath string
	SawMerger  bool
}

func ParseLine(line string, current Progress) Progress {
	line = strings.TrimSpace(stripANSI(line))
	switch {
	case strings.HasPrefix(line, "streamgrab:"):
		return parseTemplate(line, current)
	case strings.Contains(line, "[Merger]"), strings.Contains(line, "[ExtractAudio]"):
		current.Stage = "muxing"
		current.SawMerger = true
		current.Percent = 100
		if path := quotedPath(line); path != "" {
			current.OutputPath = path
		}
	case strings.Contains(line, "[download] Destination:"):
		current = advanceStage(current)
		if path := strings.TrimSpace(strings.TrimPrefix(line, "[download] Destination:")); path != "" {
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
