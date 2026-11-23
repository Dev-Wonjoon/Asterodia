package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ReadVideoTimeline indexes every displayed frame, including variable-rate video.
// Only timestamps are retained; decoded pixels are read in small preview pages.
func (e VideoEditor) ReadVideoTimeline(ctx context.Context, source string, metadata LocalMetadata) (GIFTimeline, error) {
	return e.readVideoTimeline(ctx, source, metadata, nil, 0, math.Inf(1))
}

// ReadVideoClipTimeline seeks near a selected interval and indexes only its
// displayed frames. Packet seek positions are filtered by their decoded PTS.
func (e VideoEditor) ReadVideoClipTimeline(ctx context.Context, source string, metadata LocalMetadata, start, end float64) (GIFTimeline, error) {
	if !finiteRange(start, end) || metadata.Duration == nil || metadata.Width == nil || metadata.Height == nil || end > *metadata.Duration+0.001 {
		return GIFTimeline{}, fmt.Errorf("원본 안에서 남길 영상 구간을 확인하세요")
	}
	probe, err := new(ProcessRunner).Run(ctx, ProcessRequest{Executable: e.FFprobe, Directory: filepath.Dir(source), Timeout: 30 * time.Second,
		Arguments: []string{"-v", "error", "-protocol_whitelist", "file,pipe", "-show_entries", "format=start_time", "-of", "json", source}})
	if err != nil {
		return GIFTimeline{}, err
	}
	var info struct {
		Format struct {
			Start string `json:"start_time"`
		} `json:"format"`
	}
	if err = json.Unmarshal([]byte(probe.Stdout), &info); err != nil {
		return GIFTimeline{}, err
	}
	origin, _ := strconv.ParseFloat(info.Format.Start, 64)
	if math.IsNaN(origin) || math.IsInf(origin, 0) {
		origin = 0
	}
	begin, finish := origin+start, origin+end
	interval := number(begin) + "%" + number(finish+1)
	result, err := e.readVideoTimeline(ctx, source, metadata, []string{"-read_intervals", interval}, begin, finish)
	result.StartTime -= origin
	return result, err
}

func (e VideoEditor) readVideoTimeline(ctx context.Context, source string, metadata LocalMetadata, extra []string, start, end float64) (GIFTimeline, error) {
	small := metadata.FrameCount != nil && *metadata.FrameCount > 0 && (*metadata.FrameCount <= 1000 || extra != nil && (end-start)*float64(*metadata.FrameCount) / *metadata.Duration <= 1001)
	if small && (metadata.VideoCodec == "h264" || metadata.VideoCodec == "hevc" || metadata.VideoCodec == "mpeg4" || metadata.VideoCodec == "av1") {
		if value, err := e.packetVideoTimeline(ctx, source, metadata, extra != nil, start, end); err == nil {
			return value, nil
		} else if ctx.Err() != nil || errors.Is(err, ErrStopUnconfirmed) {
			return GIFTimeline{}, err
		}
	}
	result := GIFTimeline{Width: *metadata.Width, Height: *metadata.Height, TickRate: 1000}
	first, previous, finalDuration := math.NaN(), 0.0, 0.0
	boundary := int64(0)
	args := []string{"-v", "error", "-protocol_whitelist", "file,pipe", "-select_streams", "V:0", "-show_entries", "frame=best_effort_timestamp_time,duration_time,pkt_duration_time", "-of", "compact=p=0"}
	args = append(args, extra...)
	args = append(args, source)
	_, err := new(ProcessRunner).Run(ctx, ProcessRequest{Executable: e.FFprobe, Directory: filepath.Dir(source), Unbounded: true, StreamOutput: true,
		Arguments: args,
		OnStdoutLine: func(line string) error {
			pts, duration := math.NaN(), 0.0
			frame := false
			for _, field := range strings.Split(line, "|") {
				key, value, found := strings.Cut(field, "=")
				if !found {
					continue
				}
				if key == "best_effort_timestamp_time" {
					frame = true
				}
				n, err := strconv.ParseFloat(value, 64)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
					continue
				}
				if key == "best_effort_timestamp_time" {
					pts = n
				}
				if key == "duration_time" || key == "pkt_duration_time" {
					duration = n
				}
			}
			if math.IsNaN(pts) {
				if frame {
					return fmt.Errorf("원본 프레임의 재생 시간을 읽지 못했습니다")
				}
				return nil
			}
			if extra != nil && (pts < start-0.000001 || pts >= end-0.000001) {
				return nil
			}
			if math.IsNaN(first) {
				first = pts
				if extra != nil {
					result.StartTime = pts
				}
			} else {
				next := int64(math.Round((pts - first) * 1000))
				delay := next - boundary
				if delay < 1 || delay > 0xffffff {
					return fmt.Errorf("원본 프레임 간격을 GIF·WebP 시간 단위로 표현할 수 없습니다")
				}
				result.Delays = append(result.Delays, int(delay))
				boundary = next
			}
			previous, finalDuration = pts, duration
			result.FrameTimes = append(result.FrameTimes, pts-first)
			return ctx.Err()
		},
	})
	if err != nil {
		return result, err
	}
	if math.IsNaN(first) {
		return result, fmt.Errorf("영상에 프레임이 없습니다")
	}
	if finalDuration <= 0 && len(result.Delays) > 0 {
		finalDuration = float64(result.Delays[len(result.Delays)-1]) / 1000
	}
	if finalDuration <= 0 {
		finalDuration = *metadata.Duration - (previous - first)
	}
	if extra != nil {
		finalDuration = min(finalDuration, end-previous)
	}
	last := int(math.Round(finalDuration * 1000))
	if last < 1 || last > 0xffffff {
		return result, fmt.Errorf("마지막 프레임 재생 시간을 확인할 수 없습니다")
	}
	result.Delays = append(result.Delays, last)
	return result, nil
}

func (e VideoEditor) VideoFramePage(ctx context.Context, source, dir string, count, offset, limit int) error {
	return e.VideoClipFramePage(ctx, source, dir, count, offset, limit, 0, 0)
}

func (e VideoEditor) VideoClipFramePage(ctx context.Context, source, dir string, count, offset, limit int, start, end float64, exactTime ...float64) error {
	if count < 1 || offset < 0 || offset >= count || limit < 1 {
		return fmt.Errorf("영상 프레임 범위를 확인하세요")
	}
	filter := fmt.Sprintf("trim=start_frame=%d:end_frame=%d,setpts=N/TB,scale=480:480:force_original_aspect_ratio=decrease,setsar=1", offset, min(count, offset+limit))
	if len(exactTime) == 1 && end > start && !math.IsNaN(exactTime[0]) && !math.IsInf(exactTime[0], 0) && exactTime[0] >= start && exactTime[0] < end {
		// Accurate input seeking decodes only from the preceding keyframe. A
		// one-microsecond margin avoids rounding past the indexed source frame.
		start = math.Max(start, exactTime[0]-0.000001)
		filter = "setpts=N/TB,scale=480:480:force_original_aspect_ratio=decrease,setsar=1"
	}
	args := []string{}
	if end > start {
		args = append(args, "-ss", number(start), "-t", number(end-start))
	}
	args = append(args, "-protocol_whitelist", "file,pipe", "-i", source, "-map", "0:V:0", "-an", "-sn", "-dn", "-vf", filter, "-fps_mode", "passthrough", "-frames:v", strconv.Itoa(min(limit, count-offset)), "-start_number", strconv.Itoa(offset), "-q:v", "3", filepath.Join(dir, "%012d.jpg"))
	return e.streamRun(ctx, dir, args, nil, nil, 0, nil)
}

// Indexed H.264/HEVC/MPEG-4/AV1 clips can use one packet per displayed frame.
// Validate the complete container count and all PTS values, retaining only the
// requested interval. Other layouts fall back to decoded frame timestamps.
func (e VideoEditor) packetVideoTimeline(ctx context.Context, source string, metadata LocalMetadata, clipped bool, start, end float64) (GIFTimeline, error) {
	result := GIFTimeline{Width: *metadata.Width, Height: *metadata.Height, TickRate: 1000}
	type packet struct{ pts, duration float64 }
	packets := make([]packet, 0, min(1000, *metadata.FrameCount))
	seen := 0
	_, err := new(ProcessRunner).Run(ctx, ProcessRequest{Executable: e.FFprobe, Directory: filepath.Dir(source), Timeout: 30 * time.Second, StreamOutput: true,
		Arguments: []string{"-v", "error", "-protocol_whitelist", "file,pipe", "-select_streams", "V:0", "-show_packets", "-show_entries", "packet=pts_time,duration_time,flags", "-of", "compact=p=0", source},
		OnStdoutLine: func(line string) error {
			p := packet{pts: math.NaN()}
			for _, field := range strings.Split(line, "|") {
				key, value, _ := strings.Cut(field, "=")
				if key == "flags" && strings.Contains(value, "D") {
					return fmt.Errorf("discarded packet")
				}
				if key == "pts_time" {
					var parseErr error
					p.pts, parseErr = strconv.ParseFloat(value, 64)
					if parseErr != nil {
						p.pts = math.NaN()
					}
				}
				if key == "duration_time" {
					p.duration, _ = strconv.ParseFloat(value, 64)
				}
			}
			if math.IsNaN(p.pts) || math.IsInf(p.pts, 0) || seen >= *metadata.FrameCount {
				return fmt.Errorf("packet layout does not match frames")
			}
			seen++
			if !clipped || p.pts >= start-0.000001 && p.pts < end-0.000001 {
				packets = append(packets, p)
			}
			return ctx.Err()
		}})
	if err != nil {
		return result, err
	}
	if seen != *metadata.FrameCount {
		return result, fmt.Errorf("packet count does not match frames")
	}
	sort.Slice(packets, func(i, j int) bool { return packets[i].pts < packets[j].pts })
	if len(packets) == 0 {
		return result, fmt.Errorf("no displayed frames")
	}
	first, boundary := packets[0].pts, int64(0)
	if clipped {
		result.StartTime = first
	}
	for i, p := range packets {
		if i > 0 && p.pts <= packets[i-1].pts {
			return result, fmt.Errorf("ambiguous frame timestamps")
		}
		result.FrameTimes = append(result.FrameTimes, p.pts-first)
		next := p.pts + p.duration
		if i+1 < len(packets) {
			next = packets[i+1].pts
		}
		ticks := int64(math.Round((next - first) * 1000))
		delay := ticks - boundary
		if i+1 == len(packets) {
			duration := p.duration
			if duration <= 0 && len(result.Delays) > 0 {
				duration = float64(result.Delays[len(result.Delays)-1]) / 1000
			}
			if duration <= 0 {
				duration = *metadata.Duration - (p.pts - first)
			}
			if clipped {
				duration = math.Min(duration, end-p.pts)
			}
			delay = int64(math.Round(duration * 1000))
		}
		if delay < 1 || delay > 0xffffff {
			return result, fmt.Errorf("invalid frame duration")
		}
		result.Delays = append(result.Delays, int(delay))
		boundary = ticks
	}
	return result, nil
}
