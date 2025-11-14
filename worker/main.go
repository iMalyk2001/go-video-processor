package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/imalyk/go-video-processor/pkg/job"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := loadConfig()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	worker, err := newWorker(ctx, cfg, logger)
	if err != nil {
		log.Fatalf("failed to initialise worker: %v", err)
	}

	logger.Info("starting worker", "queue", cfg.RedisQueueKey)
	if err := worker.run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("worker stopped with error", "error", err)
	}
}

type config struct {
	RedisAddr       string
	RedisPassword   string
	RedisDB         int
	RedisQueueKey   string
	MinioEndpoint   string
	MinioAccessKey  string
	MinioSecretKey  string
	MinioUseSSL     bool
	MinioRegion     string
	TempDir         string
	FFMPEGPath      string
	FFProbePath     string
	FFMPEGPreset    string
	VideoCRF        int
	AudioCodec      string
	AudioBitrate    string
	LogLevel        slog.Level
	PollTimeout     time.Duration
	ProgressBackoff time.Duration
	MaxRetries      int
}

func loadConfig() config {
	redisDB := parseInt(os.Getenv("REDIS_DB"), 0)
	logLevel := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn", "warning":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}

	tempDir := os.Getenv("WORKER_TMP_DIR")
	if tempDir == "" {
		tempDir = os.TempDir()
	}

	return config{
		RedisAddr:       valueOrDefault(os.Getenv("REDIS_ADDR"), "localhost:6379"),
		RedisPassword:   os.Getenv("REDIS_PASSWORD"),
		RedisDB:         redisDB,
		RedisQueueKey:   valueOrDefault(os.Getenv("REDIS_QUEUE_KEY"), "video:jobs:queue"),
		MinioEndpoint:   valueOrDefault(os.Getenv("MINIO_ENDPOINT"), "localhost:9000"),
		MinioAccessKey:  valueOrDefault(os.Getenv("MINIO_ACCESS_KEY"), "minio"),
		MinioSecretKey:  valueOrDefault(os.Getenv("MINIO_SECRET_KEY"), "minio123"),
		MinioUseSSL:     strings.EqualFold(os.Getenv("MINIO_USE_SSL"), "true"),
		MinioRegion:     os.Getenv("MINIO_REGION"),
		TempDir:         tempDir,
		FFMPEGPath:      valueOrDefault(os.Getenv("FFMPEG_PATH"), "ffmpeg"),
		FFProbePath:     valueOrDefault(os.Getenv("FFPROBE_PATH"), "ffprobe"),
		FFMPEGPreset:    valueOrDefault(os.Getenv("FFMPEG_PRESET"), "medium"),
		VideoCRF:        parseInt(os.Getenv("VIDEO_CRF"), 28),
		AudioCodec:      valueOrDefault(os.Getenv("AUDIO_CODEC"), "aac"),
		AudioBitrate:    valueOrDefault(os.Getenv("AUDIO_BITRATE"), "128k"),
		LogLevel:        logLevel,
		PollTimeout:     parseDuration(os.Getenv("QUEUE_POLL_TIMEOUT"), 5*time.Second),
		ProgressBackoff: parseDuration(os.Getenv("PROGRESS_UPDATE_BACKOFF"), 1*time.Second),
		MaxRetries:      parseInt(os.Getenv("WORKER_MAX_RETRIES"), 3),
	}
}

type worker struct {
	cfg    config
	logger *slog.Logger
	redis  *redis.Client
	minio  *minio.Client
}

func newWorker(ctx context.Context, cfg config, logger *slog.Logger) (*worker, error) {
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	minioClient, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: cfg.MinioUseSSL,
		Region: cfg.MinioRegion,
	})
	if err != nil {
		return nil, fmt.Errorf("minio connection: %w", err)
	}

	if err := os.MkdirAll(cfg.TempDir, 0o755); err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	return &worker{
		cfg:    cfg,
		logger: logger,
		redis:  redisClient,
		minio:  minioClient,
	}, nil
}

func (w *worker) run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		res, err := w.redis.BLPop(ctx, w.cfg.PollTimeout, w.cfg.RedisQueueKey).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			if errors.Is(err, context.Canceled) {
				return err
			}
			w.logger.Error("failed to pop from queue", "error", err)
			time.Sleep(time.Second)
			continue
		}

		if len(res) < 2 {
			continue
		}

		payload := res[1]
		var msg job.Message
		if err := json.Unmarshal([]byte(payload), &msg); err != nil {
			w.logger.Error("invalid job payload", "error", err)
			continue
		}

		w.logger.Info("received job", "job_id", msg.JobID, "input", msg.InputObject)
		if err := w.processJob(ctx, msg); err != nil {
			w.logger.Error("job failed", "job_id", msg.JobID, "error", err)
		}
	}
}

func (w *worker) processJob(ctx context.Context, msg job.Message) error {
	attempt, err := w.redis.HIncrBy(ctx, jobKey(msg.JobID), "attempts", 1).Result()
	if err != nil {
		return fmt.Errorf("increment attempts: %w", err)
	}

	if err := w.markStatus(ctx, msg.JobID, job.StatusProcessing, 0, map[string]interface{}{"error": "", "attempts": attempt}); err != nil {
		return fmt.Errorf("mark processing: %w", err)
	}

	inputPath, err := w.downloadInput(ctx, msg)
	if err != nil {
		if w.handleFailure(ctx, msg, attempt, 0, fmt.Errorf("download input: %w", err)) {
			return nil
		}
		return err
	}
	defer os.Remove(inputPath)

	outputPath, err := w.prepareOutputPath(msg)
	if err != nil {
		if w.handleFailure(ctx, msg, attempt, 0, fmt.Errorf("prepare output: %w", err)) {
			return nil
		}
		return err
	}
	defer os.Remove(outputPath)

	duration, err := w.probeDuration(ctx, inputPath)
	if err != nil {
		w.logger.Warn("duration probe failed, progress will be coarse", "job_id", msg.JobID, "error", err)
		duration = 0
	}

	progress, err := w.runFFmpeg(ctx, inputPath, outputPath, msg, duration)
	if err != nil {
		if w.handleFailure(ctx, msg, attempt, progress, err) {
			return nil
		}
		return err
	}

	if err := w.uploadOutput(ctx, outputPath, msg); err != nil {
		if w.handleFailure(ctx, msg, attempt, 100, fmt.Errorf("upload output: %w", err)) {
			return nil
		}
		return err
	}

	if err := w.markStatus(ctx, msg.JobID, job.StatusCompleted, 100, map[string]interface{}{
		"processed_file": msg.OutputObject,
		"error":          "",
		"attempts":       attempt,
	}); err != nil {
		return fmt.Errorf("mark completed: %w", err)
	}

	w.logger.Info("job completed", "job_id", msg.JobID, "output", msg.OutputObject)
	return nil
}

func (w *worker) downloadInput(ctx context.Context, msg job.Message) (string, error) {
	localPath := filepath.Join(w.cfg.TempDir, fmt.Sprintf("%s-input%s", msg.JobID, extFromObject(msg.InputObject)))
	opts := minio.GetObjectOptions{}
	if err := w.minio.FGetObject(ctx, msg.InputBucket, msg.InputObject, localPath, opts); err != nil {
		return "", err
	}
	return localPath, nil
}

func (w *worker) prepareOutputPath(msg job.Message) (string, error) {
	localPath := filepath.Join(w.cfg.TempDir, fmt.Sprintf("%s-output%s", msg.JobID, extFromObject(msg.OutputObject)))
	if err := os.Remove(localPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return localPath, nil
}

func (w *worker) probeDuration(ctx context.Context, input string) (float64, error) {
	cmd := exec.CommandContext(ctx, w.cfg.FFProbePath, "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", input)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	durationStr := strings.TrimSpace(string(output))
	if durationStr == "" {
		return 0, errors.New("empty duration")
	}
	return strconv.ParseFloat(durationStr, 64)
}

func (w *worker) runFFmpeg(ctx context.Context, inputPath, outputPath string, msg job.Message, duration float64) (int64, error) {
	args := []string{
		"-y",
		"-i", inputPath,
		"-c:v", msg.TargetCodec,
		"-preset", w.cfg.FFMPEGPreset,
		"-crf", strconv.Itoa(w.cfg.VideoCRF),
		"-c:a", w.cfg.AudioCodec,
		"-b:a", w.cfg.AudioBitrate,
		"-movflags", "+faststart",
		"-progress", "pipe:1",
		"-nostats",
		outputPath,
	}

	cmd := exec.CommandContext(ctx, w.cfg.FFMPEGPath, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 0, fmt.Errorf("stderr pipe: %w", err)
	}

	progressReader := bufio.NewScanner(stdout)
	errorReader := bufio.NewScanner(stderr)
	var stderrBuf strings.Builder

	progressCh := make(chan int64)
	errCh := make(chan error, 1)

	go w.consumeProgress(ctx, progressReader, duration, progressCh, errCh)
	go func() {
		for errorReader.Scan() {
			line := errorReader.Text()
			stderrBuf.WriteString(line)
			stderrBuf.WriteByte('\n')
		}
	}()

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("ffmpeg start: %w", err)
	}

	progress := int64(0)
loop:
	for {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			return progress, ctx.Err()
		case err, ok := <-errCh:
			if ok && err != nil {
				_ = cmd.Wait()
				return progress, err
			}
		case p, ok := <-progressCh:
			if !ok {
				break loop
			}
			progress = p
			if err := w.markStatus(ctx, msg.JobID, job.StatusProcessing, progress, map[string]interface{}{}); err != nil {
				w.logger.Warn("failed to update progress", "job_id", msg.JobID, "error", err)
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		return progress, fmt.Errorf("ffmpeg execution: %w - %s", err, strings.TrimSpace(stderrBuf.String()))
	}

	if progress < 100 {
		_ = w.markStatus(ctx, msg.JobID, job.StatusProcessing, 100, map[string]interface{}{})
		progress = 100
	}

	return progress, nil
}

func (w *worker) consumeProgress(ctx context.Context, scanner *bufio.Scanner, duration float64, progressCh chan<- int64, errCh chan<- error) {
	defer close(progressCh)
	defer close(errCh)

	var lastProgress int64
	lastEmit := time.Now()

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "=") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := parts[1]

		switch key {
		case "out_time_ms":
			if duration <= 0 {
				continue
			}
			outTimeMs, err := strconv.ParseFloat(value, 64)
			if err != nil {
				continue
			}
			current := int64(math.Min(100, math.Max(0, (outTimeMs/1000.0)/duration*100)))
			if current-lastProgress >= 1 || time.Since(lastEmit) > w.cfg.ProgressBackoff {
				lastProgress = current
				lastEmit = time.Now()
				select {
				case progressCh <- current:
				case <-ctx.Done():
					return
				}
			}
		case "progress":
			if value == "end" {
				select {
				case progressCh <- 100:
				case <-ctx.Done():
				}
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		errCh <- err
	}
}

func (w *worker) uploadOutput(ctx context.Context, outputPath string, msg job.Message) error {
	file, err := os.Open(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	contentType := mimeTypeForContainer(msg.TargetContainer)

	_, err = w.minio.PutObject(ctx, msg.OutputBucket, msg.OutputObject, file, stat.Size(), minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

func (w *worker) markStatus(ctx context.Context, jobID string, status job.Status, progress int64, extra map[string]interface{}) error {
	fields := map[string]interface{}{
		"status":     status,
		"progress":   progress,
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	for k, v := range extra {
		fields[k] = v
	}
	return w.redis.HSet(ctx, jobKey(jobID), fields).Err()
}

func (w *worker) markFailure(ctx context.Context, jobID string, progress int64, attempts int64, cause error) {
	errMsg := cause.Error()
	if len(errMsg) > 1024 {
		errMsg = errMsg[:1024]
	}
	if err := w.markStatus(ctx, jobID, job.StatusFailed, progress, map[string]interface{}{
		"error":    errMsg,
		"attempts": attempts,
	}); err != nil {
		w.logger.Error("failed to mark job failure", "job_id", jobID, "error", err)
	}
}

func (w *worker) handleFailure(ctx context.Context, msg job.Message, attempt int64, progress int64, cause error) bool {
	maxRetries := int64(w.cfg.MaxRetries)
	errMsg := cause.Error()
	if len(errMsg) > 1024 {
		errMsg = errMsg[:1024]
	}

	if maxRetries > 0 && attempt >= maxRetries {
		w.logger.Error("job failed with no retries remaining", "job_id", msg.JobID, "attempts", attempt, "error", errMsg)
		w.markFailure(ctx, msg.JobID, progress, attempt, cause)
		return false
	}

	w.logger.Warn("job failed, retrying", "job_id", msg.JobID, "attempt", attempt, "error", errMsg)
	if err := w.markStatus(ctx, msg.JobID, job.StatusQueued, 0, map[string]interface{}{
		"error":    errMsg,
		"attempts": attempt,
	}); err != nil {
		w.logger.Error("failed to update job for retry", "job_id", msg.JobID, "error", err)
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		w.logger.Error("failed to marshal job for retry", "job_id", msg.JobID, "error", err)
		w.markFailure(ctx, msg.JobID, progress, attempt, cause)
		return false
	}

	if err := w.redis.RPush(ctx, w.cfg.RedisQueueKey, payload).Err(); err != nil {
		w.logger.Error("failed to enqueue job retry", "job_id", msg.JobID, "error", err)
		w.markFailure(ctx, msg.JobID, progress, attempt, cause)
		return false
	}
	return true
}

func jobKey(id string) string {
	return fmt.Sprintf("job:%s", id)
}

func extFromObject(object string) string {
	u, err := url.Parse(object)
	if err == nil {
		object = u.Path
	}
	return filepath.Ext(object)
}

func mimeTypeForContainer(container string) string {
	switch strings.ToLower(container) {
	case "mp4":
		return "video/mp4"
	case "mov":
		return "video/quicktime"
	case "mkv":
		return "video/x-matroska"
	case "webm":
		return "video/webm"
	default:
		return "application/octet-stream"
	}
}

func parseInt(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseDuration(value string, fallback time.Duration) time.Duration {
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return d
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
func main() {
	// Listen for jobs and execute FFmpeg commands
}
