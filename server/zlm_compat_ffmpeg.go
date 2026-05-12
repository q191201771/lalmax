package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// ffmpegRecorder 管理 ffmpeg 录像进程
// 为什么用 ffmpeg：lal 内核无按需录像 API，ffmpeg 可从 RTMP 拉流写 MP4，与 ZLM 行为一致
type ffmpegRecorder struct {
	mu        sync.Mutex
	sessions  map[string]*recordSession
	outputDir string
}

type recordSession struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	app    string
	stream string
	file   string
	start  time.Time
}

func newFfmpegRecorder(outputDir string) *ffmpegRecorder {
	if outputDir == "" {
		outputDir = "./record"
	}
	return &ffmpegRecorder{
		sessions:  make(map[string]*recordSession),
		outputDir: outputDir,
	}
}

// recordKey 生成录像会话唯一标识
func recordKey(app, stream string, typ int) string {
	return fmt.Sprintf("%d/%s/%s", typ, app, stream)
}

// startRecord 启动 ffmpeg 从 RTMP 拉流并录制为 MP4
func (r *ffmpegRecorder) startRecord(rtmpAddr, app, stream string, typ int, maxSecond int) (string, error) {
	key := recordKey(app, stream, typ)

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.sessions[key]; ok {
		return "", fmt.Errorf("already recording: %s", key)
	}

	dir := filepath.Join(r.outputDir, app, stream)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create record dir: %w", err)
	}

	filename := fmt.Sprintf("%s_%s.mp4", stream, time.Now().Format("20060102_150405"))
	outPath := filepath.Join(dir, filename)

	srcURL := fmt.Sprintf("rtmp://%s/%s/%s", rtmpAddr, app, stream)

	ctx, cancel := context.WithCancel(context.Background())

	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-i", srcURL,
		"-c", "copy",
		"-movflags", "+faststart",
	}
	if maxSecond > 0 {
		args = append(args, "-t", fmt.Sprintf("%d", maxSecond))
	}
	args = append(args, "-y", outPath)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		cancel()
		return "", fmt.Errorf("ffmpeg start: %w", err)
	}

	sess := &recordSession{
		cmd:    cmd,
		cancel: cancel,
		app:    app,
		stream: stream,
		file:   outPath,
		start:  time.Now(),
	}
	r.sessions[key] = sess

	go func() {
		_ = cmd.Wait()
		r.mu.Lock()
		delete(r.sessions, key)
		r.mu.Unlock()
		Log.Infof("ffmpeg record finished. key=%s, file=%s", key, outPath)
	}()

	Log.Infof("ffmpeg record started. key=%s, file=%s, src=%s", key, outPath, srcURL)
	return outPath, nil
}

// stopRecord 终止 ffmpeg 录像进程
func (r *ffmpegRecorder) stopRecord(app, stream string, typ int) (string, error) {
	key := recordKey(app, stream, typ)

	r.mu.Lock()
	sess, ok := r.sessions[key]
	if !ok {
		r.mu.Unlock()
		return "", fmt.Errorf("not recording: %s", key)
	}
	delete(r.sessions, key)
	r.mu.Unlock()

	sess.cancel()
	_ = sess.cmd.Wait()
	Log.Infof("ffmpeg record stopped. key=%s, file=%s, duration=%s", key, sess.file, time.Since(sess.start))
	return sess.file, nil
}

// getSnap 用 ffmpeg 从指定 URL 截取一帧 JPEG 图片
func getSnap(srcURL string, timeoutSec int) ([]byte, error) {
	if timeoutSec <= 0 {
		timeoutSec = 10
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-i", srcURL,
		"-vframes", "1",
		"-f", "image2",
		"-vcodec", "mjpeg",
		"pipe:1",
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg snap: %w", err)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("ffmpeg snap: empty output")
	}

	return out, nil
}
