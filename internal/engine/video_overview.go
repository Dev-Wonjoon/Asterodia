package engine

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
)

// OverviewFrame seeks to one representative image without indexing or decoding
// the entire clip. Display pixels never control the eventual export resolution.
func (e VideoEditor) OverviewFrame(ctx context.Context, source, destination string, at float64) error {
	if at < 0 || math.IsNaN(at) || math.IsInf(at, 0) {
		return fmt.Errorf("invalid overview position")
	}
	return e.streamRun(ctx, filepath.Dir(destination), []string{
		"-ss", number(at), "-protocol_whitelist", "file,pipe", "-i", source,
		"-map", "0:V:0", "-an", "-sn", "-dn", "-vf", "scale=240:135:force_original_aspect_ratio=decrease,setsar=1",
		"-frames:v", "1", "-threads", "2", "-q:v", "6", destination,
	}, nil, nil, 0, nil)
}
